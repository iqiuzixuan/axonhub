package biz

import (
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func billingTestPrice(rate int64) *objects.ModelPrice {
	return &objects.ModelPrice{Items: []objects.ModelPriceItem{
		{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(rate))}},
		{ItemCode: objects.PriceItemCodeCompletion, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(rate * 2))}},
		{ItemCode: objects.PriceItemCodePromptCachedToken, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(rate).Div(decimal.NewFromInt(5)))}},
	}}
}

func TestRequestModelBillingSnapshotsAndChannelCost(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:billing-snapshot?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	system := NewSystemService(SystemServiceParams{Ent: db})
	service := NewUsageLogService(db, system, NewChannelServiceForTest(db))
	require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal}))
	_, err := service.PrepareBilling(ctx, "missing")
	require.ErrorContains(t, err, "no billing price")
	model := db.Model.Create().SetModelID("public-A").SetName("Public A").SetDeveloper("custom").SetIcon("").SetGroup("").SetStatus("enabled").SetModelCard(&objects.ModelCard{}).SetSettings(&objects.ModelSettings{BillingPrice: billingTestPrice(10)}).SaveX(ctx)
	snapshot, err := service.PrepareBilling(ctx, "public-A")
	require.NoError(t, err)
	require.NotEmpty(t, snapshot.PriceReference)
	channelSnapshot := &objects.RequestBilling{Price: billingTestPrice(1), At: time.Now(), PriceReference: "channel-old"}
	req := db.Request.Create().SetProjectID(1).SetModelID("route-B").SetOriginalModelID("public-A").SetBilling(snapshot).SetStatus("completed").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(ctx)
	execution := db.RequestExecution.Create().SetRequestID(req.ID).SetProjectID(1).SetChannelID(1).SetModelID("secret-C").SetCostPrice(channelSnapshot).SetFormat("openai/chat_completions").SetStatus("completed").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(ctx)
	// An in-flight request keeps the old price and policy after both change.
	db.Model.UpdateOne(model).SetSettings(&objects.ModelSettings{BillingPrice: billingTestPrice(99)}).SaveX(ctx)
	require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceRedirected}))
	usage := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000, TotalTokens: 1_100_000, PromptTokensDetails: &llm.PromptTokensDetails{CachedTokens: 200_000}}
	log, err := service.CreateUsageLogFromRequest(ctx, req, execution, usage)
	require.NoError(t, err)
	require.Equal(t, "secret-C", log.ModelID)
	require.Equal(t, "public-A", log.BillingModelID)
	require.Equal(t, "original", log.BillingModelSource)
	require.InDelta(t, 10.4, *log.TotalCost, 1e-9)
	require.InDelta(t, 1.04, *log.ChannelCost, 1e-9)
	require.Equal(t, snapshot.PriceReference, log.CostPriceReferenceID)
	service.InjectRequestUsageCost(ctx, req, execution, usage)
	require.InDelta(t, 10.4, *usage.Cost, 1e-9)
	quota := &ProviderQuotaService{AbstractService: &AbstractService{db: db}}
	amount, err := quota.channelCostSince(ctx, 1, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	require.InDelta(t, 1.04, amount, 1e-9)
	// Explicit free pricing remains distinct from missing pricing.
	require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal}))
	db.Model.UpdateOne(model).SetSettings(&objects.ModelSettings{BillingPrice: &objects.ModelPrice{Items: []objects.ModelPriceItem{}}}).SaveX(ctx)
	free, err := service.PrepareBilling(ctx, "public-A")
	require.NoError(t, err)
	_, cost, _ := requestBillingCost(free, usage)
	require.NotNil(t, cost)
	require.Zero(t, *cost)
	// Channel cost absence must not be replaced by the public charge.
	db.UsageLog.Create().SetProjectID(1).SetRequestID(req.ID).SetChannelID(2).SetModelID("other").SetBillingModelSource("original").SetTotalCost(100).SaveX(ctx)
	amount, err = quota.channelCostSince(ctx, 2, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
	require.NoError(t, err)
	require.Zero(t, amount)
}
