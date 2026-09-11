package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/enttest"
)

func TestNewEntClientUpgradesSplitUserNames(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "legacy-users.db")
	ctx := authz.WithTestBypass(context.Background())
	legacy := enttest.NewEntClient(t, "sqlite3", dsn)
	u := legacy.User.Create().SetEmail("legacy@example.com").SetPassword("migration-test-placeholder").
		SetFirstName("三").SetLastName("张").SaveX(ctx)
	require.NoError(t, legacy.Close())

	// Reproduce the deployed schema, which has no name column at all.
	raw, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, "ALTER TABLE users DROP COLUMN name")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	upgraded := NewEntClient(Config{Dialect: "sqlite3", DSN: dsn, MaxOpenConns: 1, MaxIdleConns: 1})
	got := upgraded.User.GetX(ctx, u.ID)
	require.Equal(t, "张三", got.Name)
	require.Equal(t, "三", got.FirstName)
	require.Equal(t, "张", got.LastName)
	upgraded.User.UpdateOne(got).SetName("自己的昵称").SaveX(ctx)
	require.NoError(t, upgraded.Close())

	// A second startup must not replace an explicitly edited name with old columns.
	reopened := NewEntClient(Config{Dialect: "sqlite3", DSN: dsn, MaxOpenConns: 1, MaxIdleConns: 1})
	defer reopened.Close()
	require.Equal(t, "自己的昵称", reopened.User.GetX(ctx, u.ID).Name)
}

func TestEnsureSQLiteDSN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dialect    string
		dsn        string
		disableWAL bool
		want       string
	}{
		{
			name:    "postgres unchanged",
			dialect: "postgres",
			dsn:     "postgres://localhost/axonhub",
			want:    "postgres://localhost/axonhub",
		},
		{
			name:    "sqlite adds wal and busy timeout",
			dialect: "sqlite3",
			dsn:     "file:axonhub.db?cache=shared&_fk=1",
			want:    "file:axonhub.db?cache=shared&_fk=1&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		},
		{
			name:    "sqlite without query params",
			dialect: "sqlite3",
			dsn:     "file:axonhub.db",
			want:    "file:axonhub.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)",
		},
		{
			name:       "wal disabled still adds busy timeout",
			dialect:    "sqlite3",
			dsn:        "file:axonhub.db",
			disableWAL: true,
			want:       "file:axonhub.db?_pragma=busy_timeout(5000)",
		},
		{
			name:    "existing wal preserved",
			dialect: "sqlite3",
			dsn:     "file:axonhub.db?_pragma=journal_mode(DELETE)",
			want:    "file:axonhub.db?_pragma=journal_mode(DELETE)&_pragma=busy_timeout(5000)",
		},
		{
			name:    "existing busy timeout preserved",
			dialect: "sqlite3",
			dsn:     "file:axonhub.db?_pragma=busy_timeout(10000)",
			want:    "file:axonhub.db?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)",
		},
		{
			name:    "both pragmas preserved",
			dialect: "sqlite3",
			dsn:     "file:axonhub.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)",
			want:    "file:axonhub.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := ensureSQLiteDSN(tt.dialect, tt.dsn, tt.disableWAL)
			if got != tt.want {
				t.Fatalf("ensureSQLiteDSN() = %q, want %q", got, tt.want)
			}
		})
	}
}
