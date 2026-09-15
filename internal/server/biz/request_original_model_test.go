package biz

import (
	"context"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/datastorage"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/stretchr/testify/require"
)

func TestOriginalModelFromBody(t *testing.T) {
	for _, body := range []string{`{}`, `null`, `[]`, `{"model":23}`, `{"model":{}}`, `{"model":null}`, `{"model":" "}`, `{"request":{"model":"secret"}}`, `{"MODEL":"secret"}`, `{"model":"public"} trailing`} {
		require.Empty(t, originalModelFromBody([]byte(body)), body)
	}
	require.Equal(t, "public-A", originalModelFromBody([]byte(`{"model":"public-A","messages":[{"content":"model: secret-C"}]}`)))
}

func TestOriginalModelRecoveryPreservesBillingAndProjection(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:original-model-recovery?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	svc := newResponsesSessionRequestService(db)
	p := db.Project.Create().SetName("recovery").SaveX(ctx)
	oldTime := time.Now().Add(-24 * time.Hour).UTC().Truncate(time.Second)
	legacy := db.Request.Create().SetProjectID(p.ID).SetModelID("route-B").SetStatus(request.StatusCompleted).
		SetCreatedAt(oldTime).SetUpdatedAt(oldTime).SetRequestBody([]byte(`{"model":"public-A"}`)).SaveX(ctx)
	execution := db.RequestExecution.Create().SetProjectID(p.ID).SetRequestID(legacy.ID).SetChannelID(1).
		SetModelID("secret-C").SetStatus("completed").SetFormat("openai/chat_completions").SetRequestBody([]byte(`{"model":"secret-C"}`)).SaveX(ctx)
	usage := db.UsageLog.Create().SetProjectID(p.ID).SetRequestID(legacy.ID).SetChannelID(1).
		SetModelID("secret-C").SetTotalCost(3.25).SetChannelCost(1.5).SaveX(ctx)
	known := db.Request.Create().SetProjectID(p.ID).SetModelID("route-B").SetOriginalModelID("already-recorded").
		SetStatus(request.StatusCompleted).SetRequestBody([]byte(`{"model":"do-not-overwrite"}`)).SaveX(ctx)
	missing := db.Request.Create().SetProjectID(p.ID).SetModelID("do-not-use-route").SetStatus(request.StatusCompleted).
		SetRequestBody([]byte(`{}`)).SetResponseBody([]byte(`{"model":"do-not-use-response"}`)).SaveX(ctx)
	processing := db.Request.Create().SetProjectID(p.ID).SetModelID("route").SetStatus(request.StatusProcessing).
		SetRequestBody([]byte(`{"model":"still-writing"}`)).SaveX(ctx)
	// A batch cursor crosses missing rows and already populated rows.
	next, count, err := svc.recoverOriginalModelsBatch(ctx, 0, 1)
	require.NoError(t, err)
	require.Equal(t, missing.ID, next)
	require.Zero(t, count)
	next, count, err = svc.recoverOriginalModelsBatch(ctx, next, 1)
	require.NoError(t, err)
	require.Equal(t, legacy.ID, next)
	require.Equal(t, 1, count)
	next, count, err = svc.recoverOriginalModelsBatch(ctx, next, 1)
	require.NoError(t, err)
	require.Zero(t, next)
	require.Zero(t, count)
	_, count, err = svc.recoverOriginalModelsBatch(ctx, 0, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	raw := db.Request.GetX(ctx, legacy.ID)
	require.Equal(t, "public-A", raw.OriginalModelID)
	require.Equal(t, "route-B", raw.ModelID)
	require.Equal(t, oldTime, raw.UpdatedAt)
	require.Nil(t, raw.Billing)
	require.Equal(t, legacy.RequestBody, raw.RequestBody)
	require.Equal(t, "already-recorded", db.Request.GetX(ctx, known.ID).OriginalModelID)
	require.Empty(t, db.Request.GetX(ctx, missing.ID).OriginalModelID)
	require.Empty(t, db.Request.GetX(ctx, processing.ID).OriginalModelID)
	require.Equal(t, "secret-C", db.RequestExecution.GetX(ctx, execution.ID).ModelID)
	preserved := db.UsageLog.GetX(ctx, usage.ID)
	require.Equal(t, 3.25, *preserved.TotalCost)
	require.Equal(t, 1.5, *preserved.ChannelCost)
	require.Empty(t, preserved.BillingModelSource)
	InstallModelDisplay(db)
	public := WithModelDisplay(ctx, objects.BillingModelSourceOriginal)
	require.Equal(t, "public-A", db.Request.Query().Where(request.ModelIDEQ("public-A")).OnlyX(public).ModelID)
	require.Equal(t, "public-A", db.UsageLog.GetX(public, usage.ID).ModelID)
	require.Equal(t, "public-A", db.RequestExecution.GetX(public, execution.ID).ModelID)
	var groups []struct {
		ModelID string `json:"model_id"`
		Count   int    `json:"count"`
	}
	require.NoError(t, db.UsageLog.Query().GroupBy(usagelog.FieldModelID).Aggregate(ent.Count()).Scan(public, &groups))
	require.Equal(t, "public-A", groups[0].ModelID)
	admin := WithModelDisplay(contexts.WithUser(ctx, &ent.User{IsOwner: true}), objects.BillingModelSourceOriginal)
	require.Equal(t, "secret-C", db.RequestExecution.GetX(admin, execution.ID).ModelID)
	require.Equal(t, "[original model unavailable]", db.Request.GetX(public, missing.ID).ModelID)
}

func TestOriginalModelRecoveryExternalStorageRetry(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:original-model-external?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	svc := newResponsesSessionRequestService(db)
	dir := t.TempDir()
	ds := db.DataStorage.Create().SetName("external").SetDescription("recovery test").SetType(datastorage.TypeFs).SetPrimary(false).
		SetStatus(datastorage.StatusActive).SetSettings(&objects.DataStorageSettings{Directory: &dir}).SaveX(ctx)
	row := db.Request.Create().SetProjectID(1).SetDataStorageID(ds.ID).SetModelID("route-B").SetStatus(request.StatusCompleted).
		SetRequestBody([]byte(`{}`)).SaveX(ctx)
	// A missing client body must not fall back to the stored upstream payload.
	require.NoError(t, svc.DataStorageService.SaveData(ctx, ds, GenerateExecutionRequestBodyKey(1, row.ID, 1), []byte(`{"model":"secret-C"}`)))
	_, count, err := svc.recoverOriginalModelsBatch(ctx, 0, 100)
	require.NoError(t, err)
	require.Zero(t, count)
	require.Empty(t, db.Request.GetX(ctx, row.ID).OriginalModelID)
	require.NoError(t, svc.DataStorageService.SaveData(ctx, ds, GenerateRequestBodyKey(1, row.ID), []byte(`{"model":"external-public-A"}`)))
	_, count, err = svc.recoverOriginalModelsBatch(t.Context(), 0, 100)
	require.NoError(t, err)
	require.Equal(t, 1, count)
	require.Equal(t, "external-public-A", db.Request.GetX(ctx, row.ID).OriginalModelID)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	next, _, err := svc.recoverOriginalModelsBatch(cancelled, row.ID, 100)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, row.ID, next)
}

func TestCreateRequestPreservesOriginalWithoutBillingOrBodyRetention(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:original-model-create?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	svc := newResponsesSessionRequestService(db)
	require.NoError(t, svc.SystemService.SetStoragePolicy(ctx, &StoragePolicy{}))
	for _, raw := range []*httpclient.Request{
		{JSONBody: []byte(`{"model":"public-A"}`)},
		{Body: []byte(`{"model":"public-A"}`)},
	} {
		row, err := svc.CreateRequest(ctx, &llm.Request{Model: "route-B"}, raw, llm.APIFormat("openai/chat_completions"))
		require.NoError(t, err)
		require.Equal(t, "public-A", row.OriginalModelID)
		require.Equal(t, "route-B", row.ModelID)
		require.JSONEq(t, `{}`, string(row.RequestBody))
	}
	snapshot := &objects.RequestBilling{OriginalModel: "snapshot-original", Source: objects.BillingModelSourceOriginal}
	row, err := svc.CreateRequest(ctx, &llm.Request{Model: "route-B"}, &httpclient.Request{JSONBody: []byte(`{"model":"other"}`)}, llm.APIFormat("openai/chat_completions"), snapshot)
	require.NoError(t, err)
	require.Equal(t, "snapshot-original", row.OriginalModelID)
}
