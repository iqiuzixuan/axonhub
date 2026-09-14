package biz

import (
	"slices"
	"testing"

	"entgo.io/ent/dialect/sql/schema"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/migrate"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestBillingUpgradePreservesLegacyRows(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:billing-migrate?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	// Derive the pre-feature schema using Ent's schema representation, then
	// upgrade with the normal migration path. No production migration SQL.
	tables, err := schema.CopyTables(migrate.Tables)
	require.NoError(t, err)
	removed := map[string][]string{"requests": {"original_model_id", "billing"}, "request_executions": {"cost_price"}, "usage_logs": {"channel_cost", "channel_cost_items", "billing_model_id", "billing_model_source"}}
	for _, table := range tables {
		table.Columns = slices.DeleteFunc(table.Columns, func(column *schema.Column) bool { return slices.Contains(removed[table.Name], column.Name) })
	}
	require.NoError(t, migrate.Create(ctx, db.Schema, tables, migrate.WithForeignKeys(false), migrate.WithDropColumn(true)))
	// Use raw SQL only to insert a fixture while the new Go fields do not exist.
	require.NoError(t, db.Driver().Exec(ctx, `INSERT INTO requests(id,created_at,updated_at,project_id,model_id,status,request_body) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1,'legacy-route','completed','{}')`, []any{}, nil))
	require.NoError(t, db.Driver().Exec(ctx, `INSERT INTO usage_logs(id,created_at,updated_at,request_id,project_id,channel_id,model_id,format,total_cost) VALUES(1,CURRENT_TIMESTAMP,CURRENT_TIMESTAMP,1,1,1,'legacy-secret','openai/chat_completions',3.25)`, []any{}, nil))
	require.NoError(t, db.Schema.Create(ctx, migrate.WithForeignKeys(false)))
	req, err := db.Request.Get(ctx, 1)
	require.NoError(t, err)
	require.Empty(t, req.OriginalModelID)
	require.Nil(t, req.Billing)
	log, err := db.UsageLog.Get(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 3.25, *log.TotalCost)
	require.Nil(t, log.ChannelCost)
	InstallModelDisplay(db)
	req, err = db.Request.Get(WithModelDisplay(ctx, objects.BillingModelSourceOriginal), 1)
	require.NoError(t, err)
	require.Equal(t, "[original model unavailable]", req.ModelID)
}
