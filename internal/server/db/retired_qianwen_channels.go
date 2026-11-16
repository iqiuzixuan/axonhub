package db

import (
	"context"
	"database/sql"
	"fmt"
)

// retireQianwenChannels preserves channel IDs and history while converting the
// removed custom types to their corresponding upstream protocols. It runs before
// enum DDL on MySQL/TiDB and archives even previously enabled channels so the
// removed integration cannot silently continue serving after the upgrade.
func retireQianwenChannels(ctx context.Context, db *sql.DB, dialectName string) error {
	family := dialectFamily(dialectName)
	exists, err := tableExists(ctx, db, family, "channels")
	if err != nil {
		return fmt.Errorf("check channels existence: %w", err)
	}
	if !exists {
		return nil
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("start retired channel migration: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // A committed transaction is already closed.

	query := "UPDATE channels SET type = ?, status = ? WHERE type = ?"
	if family == "postgres" {
		query = "UPDATE channels SET type = $1, status = $2 WHERE type = $3"
	}
	for _, pair := range [][2]string{{"qianwen_token_plan", "openai"}, {"qianwen_token_plan_anthropic", "anthropic"}} {
		if _, err := tx.ExecContext(ctx, query, pair[1], "archived", pair[0]); err != nil {
			return fmt.Errorf("retire channel type %s: %w", pair[0], err)
		}
	}
	return tx.Commit()
}
