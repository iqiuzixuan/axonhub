package biz

import (
	"encoding/json"
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

// No Model row is needed: both modes look up the execution channel's prices.
func TestChannelBillingModes(t *testing.T) {
	for _, tc := range []struct {
		name            string
		source          objects.BillingModelSource
		original        string
		channelID       int
		wantCost        *float64
		wantChannelCost *float64
		wantReference   string
	}{
		{"original", objects.BillingModelSourceOriginal, "public-A", 1, lo.ToPtr(10.4), lo.ToPtr(1.04), "original-price"},
		{"redirected", objects.BillingModelSourceRedirected, "public-A", 1, lo.ToPtr(1.04), lo.ToPtr(1.04), "actual-price"},
		{"same-model", objects.BillingModelSourceOriginal, "actual-B", 1, lo.ToPtr(1.04), lo.ToPtr(1.04), "actual-price"},
		{"original-missing-in-selected-channel", objects.BillingModelSourceOriginal, "other-channel-only", 1, nil, lo.ToPtr(1.04), ""},
		{"redirected-missing-in-selected-channel", objects.BillingModelSourceRedirected, "public-A", 2, nil, nil, ""},
		{"explicit-free", objects.BillingModelSourceOriginal, "free", 1, lo.ToPtr(0.0), lo.ToPtr(1.04), "free-price"},
		{"disabled-channel", objects.BillingModelSourceOriginal, "public-A", 3, nil, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := enttest.NewEntClient(t, "sqlite3", "file:billing-modes?mode=memory&_fk=0")
			defer db.Close()
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
			system := NewSystemService(SystemServiceParams{Ent: db})
			channels := NewChannelServiceForTest(db)
			channels.SetEnabledChannelsForTest([]*Channel{
				{Channel: &ent.Channel{ID: 1}, cachedModelPrices: map[string]*ent.ChannelModelPrice{
					"public-A": {Price: *billingTestPrice(10), ReferenceID: "original-price"},
					"actual-B": {Price: *billingTestPrice(1), ReferenceID: "actual-price"},
					"free":     {Price: objects.ModelPrice{Items: []objects.ModelPriceItem{}}, ReferenceID: "free-price"},
				}},
				{Channel: &ent.Channel{ID: 2}, cachedModelPrices: map[string]*ent.ChannelModelPrice{
					"other-channel-only": {Price: *billingTestPrice(99), ReferenceID: "other-channel"},
					"public-A":           {Price: *billingTestPrice(20), ReferenceID: "retry-price"},
				}},
			})
			service := NewUsageLogService(db, system, channels)
			require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: tc.source}))
			policy, err := service.PrepareBilling(ctx, tc.original)
			require.NoError(t, err)
			require.Nil(t, policy.Price, "price must wait for channel selection")
			billing, channelPrice, err := service.SnapshotRequestPrices(policy, tc.channelID, "actual-B")
			require.NoError(t, err)
			require.Equal(t, tc.source, billing.Source)
			req := db.Request.Create().SetProjectID(1).SetModelID("route-model").SetOriginalModelID(tc.original).
				SetBilling(billing).SetStatus("completed").SetRequestBody([]byte(`{}`)).SaveX(ctx)
			execution := db.RequestExecution.Create().SetRequestID(req.ID).SetProjectID(1).SetChannelID(tc.channelID).
				SetModelID("actual-B").SetCostPrice(channelPrice).SetFormat("openai/chat_completions").
				SetStatus("completed").SetRequestBody([]byte(`{}`)).SaveX(ctx)
			// Later edits must not change either saved snapshot or the response charge.
			channels.GetEnabledChannel(1).cachedModelPrices["public-A"].Price = *billingTestPrice(99)
			channels.GetEnabledChannel(1).cachedModelPrices["actual-B"].Price = *billingTestPrice(99)
			require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceRedirected}))
			usage := &llm.Usage{PromptTokens: 1_000_000, CompletionTokens: 100_000, TotalTokens: 1_100_000,
				PromptTokensDetails: &llm.PromptTokensDetails{CachedTokens: 200_000}, Cost: lo.ToPtr(999.0)}
			// Reload the persisted request/execution to cover JSON snapshot round trips.
			req = db.Request.GetX(ctx, req.ID)
			execution = db.RequestExecution.GetX(ctx, execution.ID)
			log, err := service.CreateUsageLogFromRequest(ctx, req, execution, usage)
			require.NoError(t, err)
			require.Equal(t, tc.wantCost, log.TotalCost)
			require.Equal(t, tc.wantChannelCost, log.ChannelCost)
			require.Equal(t, tc.wantReference, log.CostPriceReferenceID)
			require.EqualValues(t, tc.source, log.BillingModelSource)
			wantModel := "actual-B"
			if tc.source == objects.BillingModelSourceOriginal {
				wantModel = tc.original
			}
			require.Equal(t, wantModel, log.BillingModelID)
			require.Equal(t, "actual-B", log.ModelID)
			service.InjectRequestUsageCost(ctx, req, execution, usage)
			require.Equal(t, tc.wantCost, usage.Cost)
			quota := &ProviderQuotaService{AbstractService: &AbstractService{db: db}}
			amount, err := quota.channelCostSince(ctx, tc.channelID, time.Now().UTC().Add(-time.Hour), time.Now().UTC().Add(time.Hour))
			require.NoError(t, err)
			if tc.wantChannelCost == nil {
				require.Zero(t, amount)
			} else {
				require.Equal(t, *tc.wantChannelCost, amount)
			}

			if tc.name == "original" {
				// A retry must resolve the new channel, not retain a prior attempt's price.
				retry, _, err := service.SnapshotRequestPrices(billing, 2, "actual-B")
				require.NoError(t, err)
				_, cost, ref := requestBillingCost(retry, usage)
				require.InDelta(t, 20.8, *cost, 1e-9)
				require.Equal(t, "retry-price", ref)
				require.Equal(t, "original-price", billing.PriceReference)
				missing, _, err := service.SnapshotRequestPrices(retry, 3, "actual-B")
				require.NoError(t, err)
				require.Nil(t, missing.Price, "a missing retry price must clear the previous price")
			}
		})
	}
}

func TestRetiredModelPriceDoesNotAffectChannelBilling(t *testing.T) {
	var settings objects.ModelSettings
	require.NoError(t, json.Unmarshal([]byte(`{"billingPrice":{"items":[]},"associations":[]}`), &settings))
	raw, err := json.Marshal(settings)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "billingPrice", "retired settings are tolerated but no longer emitted")
	// Historical snapshots keep their original rates and remain readable.
	var snapshot objects.RequestBilling
	require.NoError(t, json.Unmarshal([]byte(`{"source":"original","originalModel":"legacy","price":{"items":[]},"priceReference":"model:1:legacy","at":"2026-09-15T00:00:00Z"}`), &snapshot))
	_, cost, ref := requestBillingCost(&snapshot, &llm.Usage{PromptTokens: 100})
	require.NotNil(t, cost)
	require.Zero(t, *cost)
	require.Equal(t, "model:1:legacy", ref)
}
