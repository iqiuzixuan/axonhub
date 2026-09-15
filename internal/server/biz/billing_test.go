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
	unpriced, err := service.PrepareBilling(ctx, "missing")
	require.NoError(t, err)
	require.Nil(t, unpriced.Price)
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

func TestOriginalBillingWithoutPriceAllowsUsageWithoutCharge(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:unpriced-original-billing?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	system := NewSystemService(SystemServiceParams{Ent: db})
	service := NewUsageLogService(db, system, NewChannelServiceForTest(db))
	require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal}))
	// Channel-only models such as codex-auto-review need no Model row or tariff.
	snapshot, err := service.PrepareBilling(ctx, "codex-auto-review")
	require.NoError(t, err)
	require.Equal(t, "codex-auto-review", snapshot.OriginalModel)
	require.Equal(t, objects.BillingModelSourceOriginal, snapshot.Source)
	require.Nil(t, snapshot.Price)
	require.Empty(t, snapshot.PriceReference)
	entity := db.Model.Create().SetModelID("codex-auto-review").SetName("Review").SetDeveloper("custom").SetIcon("").SetGroup("").
		SetStatus("enabled").SetModelCard(&objects.ModelCard{}).SetSettings(&objects.ModelSettings{}).SaveX(ctx)
	unpriced, err := service.PrepareBilling(ctx, entity.ModelID)
	require.NoError(t, err)
	require.Nil(t, unpriced.Price)
	// Configuring a price later must not change an already accepted request.
	db.Model.UpdateOne(entity).SetSettings(&objects.ModelSettings{BillingPrice: billingTestPrice(10)}).SaveX(ctx)
	req := db.Request.Create().SetProjectID(1).SetModelID("internal-route").SetOriginalModelID(snapshot.OriginalModel).
		SetBilling(snapshot).SetStatus("completed").SetRequestBody([]byte(`{}`)).SaveX(ctx)
	execution := db.RequestExecution.Create().SetProjectID(1).SetRequestID(req.ID).SetChannelID(1).SetModelID("internal-actual").
		SetCostPrice(&objects.RequestBilling{Price: billingTestPrice(1), At: time.Now(), PriceReference: "channel-price"}).
		SetStatus("completed").SetFormat("openai/chat_completions").SetRequestBody([]byte(`{}`)).SaveX(ctx)
	usage := &llm.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000, Cost: lo.ToPtr(999.0)}
	saved, err := service.CreateUsageLogFromRequest(ctx, req, execution, usage)
	require.NoError(t, err)
	require.Nil(t, saved.TotalCost, "unpriced is unknown, not zero or the channel cost")
	require.Empty(t, saved.CostItems)
	require.Empty(t, saved.CostPriceReferenceID)
	require.Equal(t, "original", saved.BillingModelSource)
	require.Equal(t, "codex-auto-review", saved.BillingModelID)
	require.Equal(t, 1.0, *saved.ChannelCost, "channel accounting remains independent")
	require.EqualValues(t, 1_000_000, saved.TotalTokens)
	service.InjectRequestUsageCost(ctx, req, execution, usage)
	require.Nil(t, usage.Cost, "never return upstream cost as user charge")
	priced, err := service.PrepareBilling(ctx, entity.ModelID)
	require.NoError(t, err)
	require.NotNil(t, priced.Price)
	// Invalid configured pricing is still an error, not an unpriced model.
	db.Model.UpdateOne(entity).SetSettings(&objects.ModelSettings{BillingPrice: &objects.ModelPrice{Items: []objects.ModelPriceItem{{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: "invalid"}}}}}).SaveX(ctx)
	_, err = service.PrepareBilling(ctx, entity.ModelID)
	require.ErrorContains(t, err, "billing price is invalid")
}
