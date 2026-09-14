package biz

import (
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestModelDisplayProjection(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:model-display?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	p := db.Project.Create().SetName("display").SaveX(setup)
	r := db.Request.Create().SetProjectID(p.ID).SetStatus("completed").SetModelID("route-B").SetOriginalModelID("public-A").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(setup)
	db.RequestExecution.Create().SetProjectID(p.ID).SetRequestID(r.ID).SetChannelID(1).SetModelID("secret-C").SetStatus("completed").SetFormat("openai/chat_completions").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(setup)
	db.UsageLog.Create().SetProjectID(p.ID).SetRequestID(r.ID).SetChannelID(1).SetModelID("secret-C").SetTotalTokens(10).SaveX(setup)
	InstallModelDisplay(db)
	ctx := WithModelDisplay(setup, objects.BillingModelSourceOriginal)
	got, err := db.Request.Query().Where(request.ModelIDEQ("public-A")).WithExecutions().WithUsageLogs().Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "public-A", got.ModelID)
	require.Equal(t, "public-A", got.Edges.Executions[0].ModelID)
	require.Equal(t, "public-A", got.Edges.UsageLogs[0].ModelID)
	n, err := db.Request.Query().Where(request.ModelIDEQ("route-B")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	n, err = db.RequestExecution.Query().Where(requestexecution.ModelIDEQ("secret-C")).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, n)
	var grouped []struct {
		ModelID string `json:"model_id"`
		Count   int    `json:"count"`
	}
	err = db.UsageLog.Query().GroupBy(usagelog.FieldModelID).Aggregate(ent.Count()).Scan(ctx, &grouped)
	require.NoError(t, err)
	require.Equal(t, "public-A", grouped[0].ModelID)
	redirected := WithModelDisplay(setup, objects.BillingModelSourceRedirected)
	got, err = db.Request.Get(redirected, r.ID)
	require.NoError(t, err)
	require.Equal(t, "secret-C", got.ModelID)
	admin := WithModelDisplay(contexts.WithUser(setup, &ent.User{IsOwner: true}), objects.BillingModelSourceOriginal)
	exec, err := db.RequestExecution.Query().Only(admin)
	require.NoError(t, err)
	require.Equal(t, "secret-C", exec.ModelID)
	raw, err := db.Request.Get(setup, r.ID)
	require.NoError(t, err)
	require.Equal(t, "route-B", raw.ModelID)
}
