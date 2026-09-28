package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEntClientUpgradesChannelAPIKeyIndex(t *testing.T) {
	cfg := Config{Dialect: "sqlite3", DSN: "file:" + filepath.Join(t.TempDir(), "legacy-executions.db"), MaxOpenConns: 1, MaxIdleConns: 1}
	client := NewEntClient(cfg)
	require.NoError(t, client.Close())
	raw, err := sql.Open("sqlite3", cfg.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { raw.Close() })
	// Reproduce the deployed schema before key positions were recorded.
	_, err = raw.Exec("ALTER TABLE request_executions DROP COLUMN channel_api_key_index")
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO request_executions
		(request_id, model_id, request_body, status, channel_api_key_suffix, cost_price)
		VALUES (1, 'original-model', '{}', 'completed', '1234', '{"source":"original"}')`)
	require.NoError(t, err)

	client = NewEntClient(cfg)
	require.NoError(t, client.Close())
	var index sql.NullInt64
	var suffix, model, price string
	require.NoError(t, raw.QueryRow("SELECT channel_api_key_index, channel_api_key_suffix, model_id, cost_price FROM request_executions WHERE id = 1").Scan(&index, &suffix, &model, &price))
	require.False(t, index.Valid, "historical keys must not be assigned a guessed position")
	require.Equal(t, "1234", suffix)
	require.Equal(t, "original-model", model)
	require.JSONEq(t, `{"source":"original"}`, price)
	_, err = raw.Exec("UPDATE request_executions SET channel_api_key_index = 2 WHERE id = 1")
	require.NoError(t, err)
	client = NewEntClient(cfg)
	require.NoError(t, client.Close())
	require.NoError(t, raw.QueryRow("SELECT channel_api_key_index FROM request_executions WHERE id = 1").Scan(&index))
	require.Equal(t, sql.NullInt64{Int64: 2, Valid: true}, index)
}
