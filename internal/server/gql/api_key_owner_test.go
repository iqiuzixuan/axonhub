package gql

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/99designs/gqlgen/client"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestAPIKeyStatistics_OwnersAndCosts(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:api-key-owner-stats?mode=memory&_fk=0")
	defer db.Close()
	setupCtx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	project := db.Project.Create().SetName("owner-stats").SaveX(setupCtx)
	wantNames := map[string]*string{}
	wantCosts := map[string]float64{}
	wantCounts := map[string]int{}
	for i, name := range []string{"张 三", "Alex", "", "deleted", "no-owner"} {
		keyBuilder := db.APIKey.Create().SetName("default").SetKey(fmt.Sprintf("test-key-%d", i)).SetProjectID(project.ID)
		if i == 1 {
			keyBuilder.SetStatus("archived")
		}
		var ownerName *string
		if name != "no-owner" {
			u := db.User.Create().SetEmail(fmt.Sprintf("member%d@example.com", i)).SetPassword("test").SetName(name).SaveX(setupCtx)
			keyBuilder.SetUserID(u.ID)
			if name == "deleted" {
				db.User.UpdateOne(u).SetDeletedAt(int(time.Now().UnixMilli())).SaveX(setupCtx)
			} else if name != "" {
				ownerName = new(name)
			}
		}
		key := keyBuilder.SaveX(setupCtx)
		id := fmt.Sprintf("gid://axonhub/APIKey/%d", key.ID)
		wantNames[id] = ownerName
		wantCounts[id] = i + 1
		for j := 0; j <= i; j++ {
			usage := db.UsageLog.Create().SetRequestID(i*10 + j + 1).SetModelID("test-model").
				SetProjectID(project.ID).SetAPIKeyID(key.ID).SetPromptTokens(10).SetCompletionTokens(5).SetTotalTokens(15)
			// Leave the final key's cost null to verify zero-cost statistics.
			if i != 4 {
				usage.SetTotalCost(float64(i+1) / 10)
				wantCosts[id] += float64(i+1) / 10
			}
			usage.SaveX(setupCtx)
		}
	}

	system := biz.NewSystemService(biz.SystemServiceParams{Ent: db, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	h := NewGraphqlHandlers(Dependencies{Ent: db, SystemService: system})
	for _, tc := range []struct {
		name      string
		owner     bool
		readUsers bool
	}{
		{name: "system owner", owner: true, readUsers: true},
		{name: "user reader", readUsers: true},
		{name: "dashboard only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			scopes := []string{"read_dashboard"}
			if tc.readUsers {
				scopes = append(scopes, "read_users")
			}
			viewer := db.User.Create().SetEmail(tc.name + "@example.com").SetPassword("test").SetIsOwner(tc.owner).SetScopes(scopes).SaveX(setupCtx)
			c := client.New(h.Graphql, func(req *client.Request) {
				ctx := authz.NewUserContext(ent.NewContext(req.HTTP.Context(), db), viewer.ID)
				req.HTTP = req.HTTP.WithContext(contexts.WithUser(ctx, viewer))
			})
			type stat struct {
				APIKeyID, APIKeyName string
				APIKeyUserName       *string
				Count                int
				Cost                 float64
				TotalTokens          int
			}
			var response struct {
				RequestStatsByAPIKey    []stat
				TokenStatsByAPIKey      []stat
				CostStatsByAPIKey       []stat
				AnalyticsDimensionStats []struct {
					ID, Name       string
					APIKeyUserName *string
					RequestCount   int
					Cost           float64
				}
			}
			err := c.Post(`query {
				requestStatsByAPIKey { apiKeyId apiKeyName apiKeyUserName count cost }
				tokenStatsByAPIKey { apiKeyId apiKeyName apiKeyUserName totalTokens }
				costStatsByAPIKey { apiKeyId apiKeyName apiKeyUserName cost }
				analyticsDimensionStats(dimension: "apiKey") { id name apiKeyUserName requestCount cost }
			}`, &response)
			require.NoError(t, err)
			for _, stats := range [][]stat{response.RequestStatsByAPIKey, response.TokenStatsByAPIKey, response.CostStatsByAPIKey} {
				require.Len(t, stats, 5)
				for _, item := range stats {
					require.Equal(t, "default", item.APIKeyName, "raw names must not be changed")
					if tc.readUsers {
						require.Equal(t, wantNames[item.APIKeyID], item.APIKeyUserName)
					} else {
						require.Nil(t, item.APIKeyUserName)
					}
				}
			}
			for _, item := range response.RequestStatsByAPIKey {
				require.Equal(t, wantCounts[item.APIKeyID], item.Count)
				require.InDelta(t, wantCosts[item.APIKeyID], item.Cost, 1e-9)
			}
			for _, item := range response.TokenStatsByAPIKey {
				require.Equal(t, wantCounts[item.APIKeyID]*15, item.TotalTokens)
			}
			for _, item := range response.CostStatsByAPIKey {
				require.InDelta(t, wantCosts[item.APIKeyID], item.Cost, 1e-9)
			}
			require.Len(t, response.AnalyticsDimensionStats, 5)
			for _, item := range response.AnalyticsDimensionStats {
				id := "gid://axonhub/APIKey/" + item.ID
				require.Equal(t, "default", item.Name)
				require.Equal(t, wantCounts[id], item.RequestCount)
				require.InDelta(t, wantCosts[id], item.Cost, 1e-9)
				if tc.readUsers {
					require.Equal(t, wantNames[id], item.APIKeyUserName)
				} else {
					require.Nil(t, item.APIKeyUserName)
				}
			}
		})
	}
}

func TestAPIKeyRequestStatistics_CostOutsideCostTopTen(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:api-key-top-ten?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), db))
	project := db.Project.Create().SetName("top-ten").SaveX(ctx)
	for i := range 11 {
		key := db.APIKey.Create().SetName("default").SetKey(fmt.Sprintf("test-%d", i)).SetProjectID(project.ID).SaveX(ctx)
		cost := float64(11 - i)
		if i == 10 {
			cost = 0.5
		}
		for j := 0; j <= i; j++ {
			db.UsageLog.Create().SetRequestID(i*20 + j + 1).SetModelID("model").SetProjectID(project.ID).SetAPIKeyID(key.ID).
				SetTotalCost(cost).SaveX(ctx)
		}
	}
	system := biz.NewSystemService(biz.SystemServiceParams{Ent: db, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	r := &queryResolver{&Resolver{client: db, systemService: system}}
	stats, err := r.RequestStatsByAPIKey(ctx, nil)
	require.NoError(t, err)
	require.Len(t, stats, 10)
	// The busiest key has the lowest cost. Its cost must accompany its count,
	// regardless of whether the independently ranked cost endpoint includes it.
	require.Equal(t, 11, stats[0].Count)
	require.Equal(t, 5.5, stats[0].Cost)
	costStats, err := r.CostStatsByAPIKey(ctx, nil)
	require.NoError(t, err)
	require.Len(t, costStats, 10)
	for _, costStat := range costStats {
		require.NotEqual(t, stats[0].APIKeyID, costStat.APIKeyID)
	}
}
