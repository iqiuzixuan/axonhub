package gql

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/99designs/gqlgen/client"
	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

// gqlgen's test client calls the handler in-process; no server or port is opened.
func TestPersonalGraphQLContract(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(context.Background(), db))
	u := db.User.Create().SetEmail("personal-graphql@example.com").SetPassword("secret").SetStatus(user.StatusActivated).SaveX(setup)
	p := db.Project.Create().SetName("My project").SaveX(setup)
	db.UserProject.Create().SetUserID(u.ID).SetProjectID(p.ID).SaveX(setup)
	key := db.APIKey.Create().SetName("Personal key").SetKey("must-not-be-exposed").SetType(apikey.TypePersonal).SetUserID(u.ID).SetProjectID(p.ID).SaveX(setup)
	system := biz.NewSystemService(biz.SystemServiceParams{Ent: db, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	require.NoError(t, system.SetGeneralSettings(setup, biz.SystemGeneralSettings{Timezone: "Asia/Kathmandu", CurrencyCode: "CNY"}))
	channels := biz.NewChannelServiceForTest(db)
	models := biz.NewModelService(biz.ModelServiceParams{Ent: db, SystemService: system, ChannelService: channels})
	personal := biz.NewPersonalService(db, models, system, biz.NewQuotaService(db, system))
	h := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{personalService: personal}}))
	ctx := contexts.WithUser(authz.NewUserContext(ent.NewContext(context.Background(), db), u.ID), u)
	c := client.New(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r.WithContext(ctx)) }))

	var response struct {
		MyWorkspace struct {
			Timezone     string
			CurrencyCode string
			APIKeys      []struct{ ID, Name, ProjectName string }
		}
		MyDashboard struct {
			Overview struct {
				Requests    float64
				SuccessRate *float64
			}
			Daily []struct{ Date string }
		}
	}
	err := c.Post(`query {
		myWorkspace { timezone currencyCode apiKeys { id name projectName } }
		myDashboard(input: {startDate:"2026-09-10",endDate:"2026-09-11"}) {
			overview { requests successRate } daily { date }
		}
	}`, &response)
	require.NoError(t, err)
	require.Len(t, response.MyWorkspace.APIKeys, 1)
	require.Equal(t, "Personal key", response.MyWorkspace.APIKeys[0].Name)
	require.Equal(t, "CNY", response.MyWorkspace.CurrencyCode)
	require.Equal(t, "Asia/Kathmandu", response.MyWorkspace.Timezone)
	require.Len(t, response.MyDashboard.Daily, 2)
	require.Nil(t, response.MyDashboard.Overview.SuccessRate)
	_, hasKey := contexts.GetAPIKey(ctx)
	require.False(t, hasKey)

	// Exercise real GraphQL serialization beyond 32-bit token counts, including
	// the existing quota scalar, without using a network listener.
	db.APIKey.UpdateOne(key).SetProfiles(&objects.APIKeyProfiles{ActiveProfile: "limited", Profiles: []objects.APIKeyProfile{{
		Name: "limited", Quota: &objects.APIKeyQuota{TotalTokens: lo.ToPtr(int64(5_000_000_000)),
			Period: objects.APIKeyQuotaPeriod{Type: objects.APIKeyQuotaPeriodTypeAllTime}},
	}}}).SaveX(setup)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	req := db.Request.Create().SetAPIKeyID(key.ID).SetProjectID(p.ID).SetModelID("public-alias").
		SetStatus(request.StatusCompleted).SetRequestBody(objects.JSONRawMessage("{}")).SetCreatedAt(at).SaveX(setup)
	db.UsageLog.Create().SetAPIKeyID(key.ID).SetProjectID(p.ID).SetRequestID(req.ID).SetModelID("upstream-secret").
		SetPromptTokens(3_000_000_000).SetTotalTokens(3_000_000_000).SetCreatedAt(at).SaveX(setup)
	var populated struct {
		MyDashboard struct {
			Overview struct{ TotalTokens float64 }
			APIKeys  []struct {
				Quota struct {
					Tokens float64
					Limit  struct{ TotalTokens float64 }
				}
			}
		}
	}
	require.NoError(t, c.Post(`{myDashboard(input:{startDate:"2026-09-10",endDate:"2026-09-11"}) {
		overview { totalTokens } apiKeys { quota { tokens limit { totalTokens } } }
	}}`, &populated))
	require.Equal(t, float64(3_000_000_000), populated.MyDashboard.Overview.TotalTokens)
	require.Equal(t, float64(3_000_000_000), populated.MyDashboard.APIKeys[0].Quota.Tokens)
	require.Equal(t, float64(5_000_000_000), populated.MyDashboard.APIKeys[0].Quota.Limit.TotalTokens)

	for _, query := range []string{
		`{ myModels { settings { associations { type } } } }`,
		`{ myModels { associatedChannelCount } }`,
		`{ myWorkspace { apiKeys { key } } }`,
		`{ myWorkspace(userId:"other") { today } }`,
	} {
		var denied map[string]any
		require.Error(t, c.Post(query, &denied), query)
	}
	anonymous := client.New(h)
	var denied map[string]any
	require.Error(t, anonymous.Post(`{myWorkspace { today }}`, &denied))
}
