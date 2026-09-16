package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func legacyChannelKeyDatabase(t *testing.T, keepSuffix bool) (Config, *sql.DB) {
	t.Helper()
	cfg := Config{Dialect: "sqlite3", DSN: "file:" + filepath.Join(t.TempDir(), "legacy.db"), MaxOpenConns: 1, MaxIdleConns: 1}
	client := NewEntClient(cfg)
	require.NoError(t, client.Close())
	raw, err := sql.Open("sqlite3", cfg.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { raw.Close() })
	_, err = raw.Exec("ALTER TABLE request_executions ADD COLUMN channel_api_key_masked text")
	require.NoError(t, err)
	if !keepSuffix {
		_, err = raw.Exec("ALTER TABLE request_executions DROP COLUMN channel_api_key_suffix")
		require.NoError(t, err)
	}
	return cfg, raw
}

func insertLegacyExecution(t *testing.T, raw *sql.DB, masked any) int64 {
	t.Helper()
	result, err := raw.Exec(`INSERT INTO request_executions
		(request_id, model_id, request_body, status, channel_api_key_masked, cost_price)
		VALUES (1, 'historical-model', '{}', 'completed', ?, '{"source":"original"}')`, masked)
	require.NoError(t, err)
	id, err := result.LastInsertId()
	require.NoError(t, err)
	return id
}

func TestChannelKeyMigrationSingleStartup(t *testing.T) {
	for _, keepSuffix := range []bool{false, true} {
		t.Run(map[bool]string{false: "old schema", true: "interrupted upgrade"}[keepSuffix], func(t *testing.T) {
			cfg, raw := legacyChannelKeyDatabase(t, keepSuffix)
			masks := []any{"abcd****1234", "****", "", nil, "unknown", "abcd****a中"}
			for _, value := range masks {
				insertLegacyExecution(t, raw, value)
			}
			for i := 0; i < 501; i++ {
				insertLegacyExecution(t, raw, "efgh****5678")
			}
			if keepSuffix {
				_, err := raw.Exec("UPDATE request_executions SET channel_api_key_suffix = '9999' WHERE id = 1")
				require.NoError(t, err)
			}
			// Deleted high IDs must not become available again during table rebuilds.
			_, err := raw.Exec("UPDATE sqlite_sequence SET seq = 9000 WHERE name = 'request_executions'")
			require.NoError(t, err)
			for startup := 0; startup < 2; startup++ {
				client := NewEntClient(cfg)
				require.NoError(t, client.Close())
				var oldColumns, total int
				require.NoError(t, raw.QueryRow("SELECT count(*) FROM pragma_table_info('request_executions') WHERE name = 'channel_api_key_masked'").Scan(&oldColumns))
				require.Zero(t, oldColumns)
				require.NoError(t, raw.QueryRow("SELECT count(*) FROM request_executions").Scan(&total))
				require.Equal(t, 507, total)
				for id, want := range map[int]sql.NullString{
					1: {String: map[bool]string{false: "1234", true: "9999"}[keepSuffix], Valid: true},
					2: {}, 3: {}, 4: {}, 5: {}, 6: {String: "a中", Valid: true},
					507: {String: "5678", Valid: true},
				} {
					var suffix sql.NullString
					var model, price string
					require.NoError(t, raw.QueryRow("SELECT channel_api_key_suffix, model_id, cost_price FROM request_executions WHERE id = ?", id).Scan(&suffix, &model, &price))
					require.Equal(t, want, suffix)
					require.Equal(t, "historical-model", model)
					require.JSONEq(t, `{"source":"original"}`, price)
				}
				var sequence int
				require.NoError(t, raw.QueryRow("SELECT seq FROM sqlite_sequence WHERE name = 'request_executions'").Scan(&sequence))
				require.Equal(t, 9000, sequence)
			}
		})
	}
}

func TestChannelKeyMigrationFailurePreservesSource(t *testing.T) {
	cfg, raw := legacyChannelKeyDatabase(t, true)
	insertLegacyExecution(t, raw, "abcd****1234")
	insertLegacyExecution(t, raw, "efgh****5678")
	_, err := raw.Exec(`CREATE TRIGGER reject_suffix BEFORE UPDATE OF channel_api_key_suffix ON request_executions
		WHEN NEW.id = 2 BEGIN SELECT RAISE(ABORT, 'injected migration failure'); END`)
	require.NoError(t, err)
	require.Panics(t, func() { NewEntClient(cfg) })
	var sources, migrated int
	require.NoError(t, raw.QueryRow("SELECT count(channel_api_key_masked), count(channel_api_key_suffix) FROM request_executions").Scan(&sources, &migrated))
	require.Equal(t, 2, sources)
	require.Zero(t, migrated, "the failed backfill must roll back its earlier updates")
	_, err = raw.Exec("DROP TRIGGER reject_suffix")
	require.NoError(t, err)
	client := NewEntClient(cfg)
	require.NoError(t, client.Close())
	require.NoError(t, raw.QueryRow("SELECT count(channel_api_key_suffix) FROM request_executions").Scan(&migrated))
	require.Equal(t, 2, migrated)
}

func TestChannelKeyMigrationDisabled(t *testing.T) {
	cfg, raw := legacyChannelKeyDatabase(t, false)
	insertLegacyExecution(t, raw, "abcd****1234")
	cfg.DisableAutoMigration = true
	client := NewEntClient(cfg)
	require.NoError(t, client.Close())
	var source string
	require.NoError(t, raw.QueryRowContext(context.Background(), "SELECT channel_api_key_masked FROM request_executions").Scan(&source))
	require.Equal(t, "abcd****1234", source)
	var newColumns int
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM pragma_table_info('request_executions') WHERE name = 'channel_api_key_suffix'").Scan(&newColumns))
	require.Zero(t, newColumns)
}
