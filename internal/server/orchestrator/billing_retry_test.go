package orchestrator

import (
	"fmt"
	"testing"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/transformer/openai"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBillingSnapshotRefreshesForEachExecution(t *testing.T) {
	for _, source := range []objects.BillingModelSource{objects.BillingModelSourceOriginal, objects.BillingModelSourceRedirected} {
		t.Run(string(source), func(t *testing.T) {
			db := enttest.NewEntClient(t, "sqlite3", "file:billing-retries?mode=memory&_fk=0")
			defer db.Close()
			ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
			channels, requests, system, usageService := setupTestServices(t, db)
			usageService.ChannelService = channels
			require.NoError(t, system.SetModelSettings(ctx, biz.SystemModelSettings{BillingModelSource: source}))
			require.NoError(t, system.SetInjectUsageCostEnabled(ctx, true))
			project := createTestProject(t, ctx, db)
			first := createTestChannel(t, ctx, db)
			db.Channel.UpdateOne(first).SetName("First channel").SaveX(ctx)
			second := createTestChannel(t, ctx, db)
			policy, err := usageService.PrepareBilling(ctx, "public-A")
			require.NoError(t, err)
			req := db.Request.Create().SetProjectID(project.ID).SetModelID("route-B").SetOriginalModelID("public-A").SetBilling(policy).SetStatus("processing").SetRequestBody([]byte(`{}`)).SaveX(ctx)
			candidates := []*ChannelModelsCandidate{}
			enabled := []*biz.Channel{}
			for _, row := range []*ent.Channel{first, second} {
				outbound, err := openai.NewOutboundTransformer(row.BaseURL, row.Credentials.APIKey)
				require.NoError(t, err)
				ch := &biz.Channel{Channel: row, Outbound: outbound}
				enabled = append(enabled, ch)
				candidates = append(candidates, &ChannelModelsCandidate{Channel: ch, Models: []biz.ChannelModelEntry{{ActualModel: "gpt-4"}}})
			}
			channels.SetEnabledChannelsForTest(enabled)
			state := &PersistenceState{Billing: policy, Request: req, RequestService: requests, UsageLogService: usageService, ChannelModelsCandidates: candidates}
			middleware := &persistRequestExecutionMiddleware{outbound: &PersistentOutboundTransformer{state: state, wrapped: enabled[0].Outbound}}
			// The final attempt is deliberately unpriced. Same-channel retries must also refresh.
			for i, attempt := range []struct {
				candidate int
				rate      int64
			}{{0, 10}, {1, 20}, {1, 30}, {0, 0}} {
				ch := enabled[attempt.candidate]
				db.ChannelModelPrice.Delete().ExecX(schematype.SkipSoftDelete(ctx))
				if attempt.rate != 0 {
					for model, rate := range map[string]int64{"public-A": attempt.rate, "gpt-4": attempt.rate * 2} {
						db.ChannelModelPrice.Create().SetChannelID(ch.ID).SetModelID(model).SetReferenceID(fmt.Sprintf("%s-%d", model, i)).
							SetPrice(objects.ModelPrice{Items: []objects.ModelPriceItem{{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(rate))}}}}).SaveX(ctx)
					}
				}
				channels.PreloadModelPricesForTest(ctx, ch)
				state.RequestExec = nil
				state.CurrentCandidateIndex = attempt.candidate
				state.CurrentCandidate = candidates[attempt.candidate]
				_, err := middleware.OnOutboundRawRequest(ctx, &httpclient.Request{APIFormat: string(llm.APIFormatOpenAIChatCompletion), Body: []byte(`{"model":"gpt-4"}`)})
				require.NoError(t, err, "attempt %d", i)
				stored := db.Request.GetX(ctx, req.ID)
				require.Equal(t, ch.ID, stored.ChannelID)
				require.Equal(t, state.Billing.Source, stored.Billing.Source)
				require.Equal(t, state.Billing.OriginalModel, stored.Billing.OriginalModel)
				require.Equal(t, state.Billing.PriceReference, stored.Billing.PriceReference)
				require.Equal(t, state.Billing.Price, stored.Billing.Price)
				require.True(t, state.Billing.At.Equal(stored.Billing.At))
				usage := &llm.Usage{PromptTokens: 1_000_000, TotalTokens: 1_000_000}
				saved, err := usageService.CreateUsageLogFromRequest(ctx, stored, state.RequestExec, usage)
				require.NoError(t, err)
				usageService.InjectRequestUsageCost(ctx, stored, state.RequestExec, usage)
				raw := state.publicResponseWithCost([]byte(`{"model":"gpt-4","usage":{"prompt_tokens":1000000,"cost":999}}`), &llm.Usage{})
				if attempt.rate == 0 {
					require.Nil(t, stored.Billing.Price)
					require.Nil(t, saved.TotalCost)
					require.Nil(t, usage.Cost)
					require.False(t, gjson.GetBytes(raw, "usage.cost").Exists())
				} else {
					want := float64(attempt.rate)
					if source == objects.BillingModelSourceRedirected {
						want *= 2
					}
					require.Equal(t, want, *saved.TotalCost)
					require.Equal(t, float64(attempt.rate*2), *saved.ChannelCost)
					require.Equal(t, want, *usage.Cost)
					require.Equal(t, want, gjson.GetBytes(raw, "usage.cost").Float())
				}
			}
			// Retry updates must not rewrite previously stored charges.
			logs := db.UsageLog.Query().Order(ent.Asc("id")).AllX(ctx)
			require.Len(t, logs, 4)
			firstCharge := 10.0
			if source == objects.BillingModelSourceRedirected {
				firstCharge *= 2
			}
			require.Equal(t, firstCharge, *logs[0].TotalCost)
		})
	}
}
