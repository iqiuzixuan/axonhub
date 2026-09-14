package gql

import (
	"fmt"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/stretchr/testify/require"
)

func TestBillingPolicyGraphQLVisibilityAndAggregation(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:billing-gql?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	system := biz.NewSystemService(biz.SystemServiceParams{Ent: db})
	require.NoError(t, system.SetModelSettings(setup, biz.SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal}))
	p := db.Project.Create().SetName("billing").SaveX(setup)
	for _, actual := range []string{"secret-C", "secret-D"} {
		req := db.Request.Create().SetProjectID(p.ID).SetModelID("route-B").SetOriginalModelID("public-A").SetStatus("completed").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(setup)
		db.RequestExecution.Create().SetProjectID(p.ID).SetRequestID(req.ID).SetChannelID(1).SetModelID(actual).SetMetricsLatencyMs(1000).SetMetricsFirstTokenLatencyMs(10).SetStream(true).SetStatus("completed").SetFormat("openai/chat_completions").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(setup)
		db.UsageLog.Create().SetProjectID(p.ID).SetRequestID(req.ID).SetChannelID(1).SetModelID(actual).SetTotalCost(3).SetCompletionTokens(10).SaveX(setup)
	}
	h := NewGraphqlHandlers(Dependencies{Ent: db, SystemService: system, ModelService: biz.NewModelService(biz.ModelServiceParams{Ent: db, SystemService: system})})
	viewer := &ent.User{ID: 1, Scopes: []string{"read_requests", "read_dashboard"}}
	viewer.Edges.ProjectUsers = []*ent.UserProject{{ProjectID: p.ID, Scopes: []string{"read_requests", "read_dashboard"}}}
	gql := client.New(h.Graphql, func(req *client.Request) {
		ctx := authz.NewUserContext(ent.NewContext(req.HTTP.Context(), db), viewer.ID)
		ctx = contexts.WithUser(ctx, viewer)
		req.HTTP = req.HTTP.WithContext(ctx)
	})
	var data struct {
		BillingModelSource string
		Requests           struct {
			Edges []struct {
				Node struct {
					ModelID          string
					RequestedModelID *string
					Executions       struct {
						Edges []struct{ Node struct{ ModelID string } }
					}
				}
			}
		}
	}
	query := `{ billingModelSource requests(first:10, where:{modelID:"public-A"}) {edges{node{modelID requestedModelID executions(first:10){edges{node{modelID}}}}}} }`
	require.NoError(t, gql.Post(query, &data))
	require.Equal(t, "original", data.BillingModelSource)
	require.Len(t, data.Requests.Edges, 2)
	for _, edge := range data.Requests.Edges {
		require.Equal(t, "public-A", edge.Node.ModelID)
		require.Nil(t, edge.Node.RequestedModelID)
		require.Equal(t, "public-A", edge.Node.Executions.Edges[0].Node.ModelID)
	}
	var stats struct {
		RequestStatsByModel []struct {
			ModelId string
			Count   int
		}
	}
	require.NoError(t, gql.Post(`{requestStatsByModel{modelId count}}`, &stats))
	require.Len(t, stats.RequestStatsByModel, 1)
	require.Equal(t, "public-A", stats.RequestStatsByModel[0].ModelId)
	require.Equal(t, 2, stats.RequestStatsByModel[0].Count)
	var performance struct {
		ModelPerformanceStats []struct {
			ModelId      string
			RequestCount int
		}
	}
	require.NoError(t, gql.Post(`{modelPerformanceStats{modelId requestCount}}`, &performance))
	require.Len(t, performance.ModelPerformanceStats, 1)
	require.Equal(t, "public-A", performance.ModelPerformanceStats[0].ModelId)
	require.Equal(t, 2, performance.ModelPerformanceStats[0].RequestCount)

	err := gql.Post(`{requests(where:{hasExecutionsWith:[{modelID:"secret-C"}]}){edges{node{id}}}}`, &data)
	require.ErrorContains(t, err, "internal execution relations")
	viewer.IsOwner = true
	require.NoError(t, gql.Post(query, &data))
	require.Equal(t, "public-A", *data.Requests.Edges[0].Node.RequestedModelID)
	require.Contains(t, []string{"secret-C", "secret-D"}, data.Requests.Edges[0].Node.Executions.Edges[0].Node.ModelID)
	// Verify the same nested input/output used by the price editor, including
	// clearing a configured price back to "unpriced".
	entity := db.Model.Create().SetModelID("priced-A").SetName("Priced A").SetDeveloper("custom").SetIcon("").SetGroup("").SetModelCard(&objects.ModelCard{}).SetSettings(&objects.ModelSettings{}).SaveX(setup)
	var priceResult struct {
		UpdateModel struct {
			Settings struct {
				BillingPrice *struct {
					Items []struct {
						ItemCode string
						Pricing  struct {
							Mode         string
							UsagePerUnit float64
						}
					}
				}
			}
		}
	}
	id := fmt.Sprintf("gid://axonhub/Model/%d", entity.ID)
	input := map[string]any{"settings": map[string]any{"associations": []any{}, "billingPrice": map[string]any{"items": []any{map[string]any{"itemCode": "prompt_tokens", "pricing": map[string]any{"mode": "usage_per_unit", "usagePerUnit": "10"}}}}}}
	mutation := `mutation($id:ID!,$input:UpdateModelInput!){updateModel(id:$id,input:$input){settings{billingPrice{items{itemCode pricing{mode usagePerUnit}}}}}}`
	require.NoError(t, gql.Post(mutation, &priceResult, client.Var("id", id), client.Var("input", input)))
	require.NotNil(t, priceResult.UpdateModel.Settings.BillingPrice)
	require.Equal(t, float64(10), priceResult.UpdateModel.Settings.BillingPrice.Items[0].Pricing.UsagePerUnit)
	input["settings"].(map[string]any)["billingPrice"] = nil
	require.NoError(t, gql.Post(mutation, &priceResult, client.Var("id", id), client.Var("input", input)))
	require.Nil(t, db.Model.GetX(setup, entity.ID).Settings.BillingPrice)
	// Configuration changes affect display, but never recalculate stored amounts.
	require.NoError(t, system.SetModelSettings(setup, biz.SystemModelSettings{BillingModelSource: objects.BillingModelSourceRedirected}))
	require.NoError(t, gql.Post(`{requestStatsByModel{modelId count}}`, &stats))
	require.Len(t, stats.RequestStatsByModel, 2)
	for _, row := range db.UsageLog.Query().AllX(setup) {
		require.Equal(t, float64(3), *row.TotalCost)
	}
}

func TestBillingSourcePreservedForOlderSettingsClients(t *testing.T) {
	resolver, ctx, db := setupTestSystemModelSettingsResolver(t)
	defer db.Close()
	require.NoError(t, resolver.systemService.SetModelSettings(ctx, biz.SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal}))
	_, err := resolver.UpdateSystemModelSettings(ctx, biz.SystemModelSettings{QueryAllChannelModels: true})
	require.NoError(t, err)
	settings, err := resolver.systemService.ModelSettings(ctx)
	require.NoError(t, err)
	require.Equal(t, objects.BillingModelSourceOriginal, settings.BillingModelSource)
}
