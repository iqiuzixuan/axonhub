package db

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRetireQianwenChannelsPreservesHistoryAndArchives(t *testing.T) {
	db := openQuotaDB(t)
	_, err := db.Exec(`CREATE TABLE channels (id INTEGER PRIMARY KEY, type TEXT NOT NULL, status TEXT NOT NULL, name TEXT NOT NULL)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO channels VALUES
		(1, 'qianwen_token_plan', 'archived', 'old'),
		(2, 'qianwen_token_plan_anthropic', 'enabled', 'active'),
		(3, 'openai', 'enabled', 'unrelated')`)
	require.NoError(t, err)

	for range 2 {
		require.NoError(t, retireQianwenChannels(context.Background(), db, "sqlite"))
	}
	rows, err := db.Query(`SELECT id, type, status, name FROM channels ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	var got [][]any
	for rows.Next() {
		var id int
		var typ, status, name string
		require.NoError(t, rows.Scan(&id, &typ, &status, &name))
		got = append(got, []any{id, typ, status, name})
	}
	require.NoError(t, rows.Err())
	require.Equal(t, [][]any{{1, "openai", "archived", "old"}, {2, "anthropic", "archived", "active"}, {3, "openai", "enabled", "unrelated"}}, got)
}
