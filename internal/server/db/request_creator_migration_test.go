package db

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewEntClientUpgradesPlaygroundCreator(t *testing.T) {
	cfg := Config{Dialect: "sqlite3", DSN: "file:" + filepath.Join(t.TempDir(), "legacy-requests.db"), MaxOpenConns: 1, MaxIdleConns: 1}
	client := NewEntClient(cfg)
	require.NoError(t, client.Close())
	raw, err := sql.Open("sqlite3", cfg.DSN)
	require.NoError(t, err)
	t.Cleanup(func() { raw.Close() })
	_, err = raw.Exec("DROP INDEX requests_by_project_id_user_id_created_at")
	require.NoError(t, err)
	_, err = raw.Exec("ALTER TABLE requests DROP COLUMN user_id")
	require.NoError(t, err)
	_, err = raw.Exec(`INSERT INTO requests (id, source, model_id, original_model_id, request_body, status, billing)
 VALUES (1,'playground','upstream-model','client-model','{}','completed','{"source":"original"}'),
 (2,'api','upstream-model','client-model','{}','completed','{"source":"original"}')`)
	require.NoError(t, err)
	client = NewEntClient(cfg)
	require.NoError(t, client.Close())
	var count int
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM requests WHERE user_id IS NULL AND original_model_id='client-model'").Scan(&count))
	require.Equal(t, 2, count, "upgrades must preserve history without guessing a creator")
	var billing string
	require.NoError(t, raw.QueryRow("SELECT billing FROM requests WHERE id=1").Scan(&billing))
	require.JSONEq(t, `{"source":"original"}`, billing)
	require.NoError(t, raw.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name='requests_by_project_id_user_id_created_at'").Scan(&count))
	require.Equal(t, 1, count)
	_, err = raw.Exec("UPDATE requests SET user_id=17 WHERE id=1")
	require.NoError(t, err)
	client = NewEntClient(cfg)
	require.NoError(t, client.Close())
	var creator int
	require.NoError(t, raw.QueryRow("SELECT user_id FROM requests WHERE id=1").Scan(&creator))
	require.Equal(t, 17, creator, "repeat upgrades must keep recorded creators")
}
