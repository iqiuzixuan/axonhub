package db

import (
	"context"
	"database/sql"
	"fmt"
	"unicode/utf8"

	amigrate "ariga.io/atlas/sql/migrate"
	aschema "ariga.io/atlas/sql/schema"
	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	eschema "entgo.io/ent/dialect/sql/schema"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/migrate"
	"github.com/looplj/axonhub/internal/ent/migrate/schemahook"
)

// migrateSchema replaces the fork's masked key column within one startup.
// First let Ent add the upstream suffix while preserving the migration source;
// only a successful backfill permits the normal Ent migration to drop it.
func migrateSchema(ctx context.Context, masterDB *sql.DB, dialectName string) error {
	// Both schema inspection and backfill must use master, never a lagging read
	// replica. This client borrows masterDB; the application owns its lifetime.
	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialectName, masterDB)))
	var legacyColumn bool
	opts := []eschema.MigrateOption{
		migrate.WithGlobalUniqueID(false),
		migrate.WithForeignKeys(false),
		migrate.WithDropIndex(true),
		migrate.WithDropColumn(true),
		eschema.WithHooks(schemahook.V0_3_0),
		filterEquivalentDefaultChanges(),
		preserveChannelKeySequence(&legacyColumn, dialectName),
	}
	firstPass := append(append([]eschema.MigrateOption{}, opts...), preserveLegacyChannelKeyColumn(&legacyColumn))
	if err := client.Schema.Create(ctx, firstPass...); err != nil {
		return err
	}
	if !legacyColumn {
		return nil
	}
	// Use the master connection directly: the legacy column has no generated
	// field, soft-deleted executions must be included, and replicas may lag.
	if err := backfillChannelKeySuffix(ctx, masterDB, dialectName); err != nil {
		return err
	}
	return client.Schema.Create(ctx, opts...)
}

// Atlas rebuilds SQLite tables to remove columns. Preserve the high-water ID in
// the same schema transaction, including IDs whose executions were deleted.
func preserveChannelKeySequence(legacy *bool, dialectName string) eschema.MigrateOption {
	return eschema.WithApplyHook(func(next eschema.Applier) eschema.Applier {
		return eschema.ApplyFunc(func(ctx context.Context, conn dialect.ExecQuerier, plan *amigrate.Plan) error {
			if !*legacy || dialectName != dialect.SQLite {
				return next.Apply(ctx, conn, plan)
			}
			var rows entsql.Rows
			if err := conn.Query(ctx, "SELECT seq FROM sqlite_sequence WHERE name = 'request_executions'", []any{}, &rows); err != nil {
				return err
			}
			var sequence int64
			if rows.Next() {
				if err := rows.Scan(&sequence); err != nil {
					rows.Close()
					return err
				}
			}
			err := rows.Err()
			closeErr := rows.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if err := next.Apply(ctx, conn, plan); err != nil {
				return err
			}
			if err := conn.Exec(ctx, "UPDATE sqlite_sequence SET seq = ? WHERE name = 'request_executions' AND seq < ?", []any{sequence, sequence}, nil); err != nil {
				return err
			}
			return conn.Exec(ctx, "INSERT INTO sqlite_sequence (name, seq) SELECT 'request_executions', ? WHERE NOT EXISTS (SELECT 1 FROM sqlite_sequence WHERE name = 'request_executions')", []any{sequence}, nil)
		})
	})
}

func preserveLegacyChannelKeyColumn(found *bool) eschema.MigrateOption {
	return eschema.WithDiffHook(func(next eschema.Differ) eschema.Differ {
		return eschema.DiffFunc(func(current, desired *aschema.Schema) ([]aschema.Change, error) {
			if table, ok := current.Table("request_executions"); ok {
				if column, ok := table.Column("channel_api_key_masked"); ok {
					*found = true
					if target, ok := desired.Table(table.Name); ok {
						// Preserve it in the desired table itself, including when
						// SQLite must rebuild a table for another schema change.
						if _, exists := target.Column(column.Name); !exists {
							copy := *column
							target.AddColumns(&copy)
						}
					}
				}
			}
			return next.Diff(current, desired)
		})
	})
}

func backfillChannelKeySuffix(ctx context.Context, masterDB *sql.DB, dialectName string) error {
	tx, err := masterDB.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start channel key suffix migration: %w", err)
	}
	defer tx.Rollback()

	builder := entsql.Dialect(dialectName)
	lastID := 0
	for {
		query, args := builder.Select("id", "channel_api_key_masked", "channel_api_key_suffix").
			From(builder.Table("request_executions")).
			Where(entsql.And(entsql.GT("id", lastID), entsql.NotNull("channel_api_key_masked"))).
			OrderBy("id").Limit(500).Query()
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("read legacy channel keys: %w", err)
		}
		type execution struct {
			id     int
			masked string
			suffix sql.NullString
		}
		batch := make([]execution, 0, 500)
		for rows.Next() {
			var row execution
			if err := rows.Scan(&row.id, &row.masked, &row.suffix); err != nil {
				rows.Close()
				return fmt.Errorf("scan legacy channel key: %w", err)
			}
			batch = append(batch, row)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return fmt.Errorf("read legacy channel keys: %w", err)
		}
		if closeErr != nil {
			return fmt.Errorf("close legacy channel keys: %w", closeErr)
		}
		if len(batch) == 0 {
			break
		}
		for _, row := range batch {
			lastID = row.id
			if row.suffix.Valid {
				continue
			}
			suffix := legacyChannelKeySuffix(row.masked)
			if suffix == "" {
				continue
			}
			query, args := builder.Update("request_executions").Set("channel_api_key_suffix", suffix).
				Where(entsql.And(entsql.EQ("id", row.id), entsql.IsNull("channel_api_key_suffix"))).Query()
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("backfill channel key suffix for execution %d: %w", row.id, err)
			}
		}
	}
	return tx.Commit()
}

func legacyChannelKeySuffix(masked string) string {
	// The removed implementation stored four bytes, four asterisks, then four
	// bytes. Empty values, "****" and unknown formats contain no reliable suffix.
	if len(masked) != 12 || masked[4:8] != "****" || !utf8.ValidString(masked[8:]) {
		return ""
	}
	return masked[8:]
}
