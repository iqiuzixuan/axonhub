package biz

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

type personalTestEnv struct {
	client   *ent.Client
	setup    context.Context
	ctx      context.Context
	svc      *PersonalService
	user     *ent.User
	project  *ent.Project
	key      *ent.APIKey
	channels *ChannelService
}

func newPersonalTestEnv(t *testing.T) *personalTestEnv {
	t.Helper()
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { _ = client.Close() })
	setup := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	u := client.User.Create().SetEmail("alice@example.com").SetPassword("secret").
		SetName("Alice").SetStatus(user.StatusActivated).SaveX(setup)
	p := client.Project.Create().SetName("Shared project").SaveX(setup)
	client.UserProject.Create().SetUserID(u.ID).SetProjectID(p.ID).SaveX(setup)
	key := client.APIKey.Create().SetName("Alice personal").SetKey("never-return-this-secret").
		SetType(apikey.TypePersonal).SetUserID(u.ID).SetProjectID(p.ID).SaveX(setup)
	system := NewSystemService(SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	channels := NewChannelServiceForTest(client)
	models := NewModelService(ModelServiceParams{Ent: client, SystemService: system, ChannelService: channels})
	ctx := contexts.WithUser(authz.NewUserContext(ent.NewContext(context.Background(), client), u.ID), u)
	return &personalTestEnv{client: client, setup: setup, ctx: ctx, svc: NewPersonalService(client, models, system, NewQuotaService(client, system)), user: u, project: p, key: key, channels: channels}
}

func (e *personalTestEnv) addRequest(key *ent.APIKey, status request.Status, at time.Time) *ent.Request {
	return e.client.Request.Create().SetAPIKeyID(key.ID).SetProjectID(key.ProjectID).
		SetModelID("public-alias").SetStatus(status).SetRequestBody(objects.JSONRawMessage("{}")).
		SetCreatedAt(at).SaveX(e.setup)
}

func TestPersonalDashboardOwnershipAndMetrics(t *testing.T) {
	e := newPersonalTestEnv(t)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	for _, status := range []request.Status{request.StatusCompleted, request.StatusFailed, request.StatusCanceled, request.StatusProcessing} {
		e.addRequest(e.key, status, at)
	}
	success := e.addRequest(e.key, request.StatusCompleted, at)
	e.client.UsageLog.Create().SetRequestID(success.ID).SetAPIKeyID(e.key.ID).SetProjectID(e.project.ID).
		SetModelID("private-upstream-name").SetPromptTokens(3_000_000_000).SetCompletionTokens(10).
		SetPromptCachedTokens(100).SetCompletionReasoningTokens(4).SetTotalTokens(3_000_000_010).
		SetTotalCost(1.25).SetCreatedAt(at).SaveX(e.setup)
	// A second usage entry must not inflate the logical request counter.
	e.client.UsageLog.Create().SetRequestID(success.ID).SetAPIKeyID(e.key.ID).SetProjectID(e.project.ID).
		SetModelID("private-upstream-name").SetPromptTokens(5).SetCompletionTokens(2).SetTotalTokens(7).
		SetCreatedAt(at).SaveX(e.setup)

	bob := e.client.User.Create().SetEmail("bob@example.com").SetPassword("secret").SetStatus(user.StatusActivated).SaveX(e.setup)
	bobKey := e.client.APIKey.Create().SetName("Bob").SetKey("bob-key").SetUserID(bob.ID).
		SetProjectID(e.project.ID).SetType(apikey.TypePersonal).SaveX(e.setup)
	e.addRequest(bobKey, request.StatusCompleted, at)
	for _, typ := range []apikey.Type{apikey.TypeUser, apikey.TypeServiceAccount} {
		key := e.client.APIKey.Create().SetName(string(typ)).SetKey(string(typ)).
			SetUserID(e.user.ID).SetProjectID(e.project.ID).SetType(typ).SaveX(e.setup)
		e.addRequest(key, request.StatusCompleted, at)
	}
	deleted := e.client.APIKey.Create().SetName("Deleted").SetKey("deleted-key").SetUserID(e.user.ID).
		SetProjectID(e.project.ID).SetType(apikey.TypePersonal).SaveX(e.setup)
	e.addRequest(deleted, request.StatusFailed, at)
	require.NoError(t, e.client.APIKey.DeleteOneID(deleted.ID).Exec(e.setup))

	stats, err := e.svc.Dashboard(e.ctx, PersonalUsageInput{StartDate: "2026-09-10", EndDate: "2026-09-11"})
	require.NoError(t, err)
	require.Equal(t, float64(6), stats.Overview.Requests)
	require.Equal(t, float64(2), stats.Overview.SuccessfulRequests)
	require.Equal(t, float64(2), stats.Overview.FailedRequests)
	require.Equal(t, float64(1), stats.Overview.CanceledRequests)
	require.Equal(t, float64(1), stats.Overview.PendingRequests)
	require.Equal(t, float64(50), *stats.Overview.SuccessRate)
	require.Equal(t, float64(3_000_000_017), stats.Overview.TotalTokens)
	require.Equal(t, float64(100), stats.Overview.CachedTokens)
	require.Equal(t, 1.25, stats.Overview.Cost)
	require.Equal(t, float64(1), stats.Overview.UnpricedUsageCount)
	require.Len(t, stats.APIKeys, 2)
	require.True(t, stats.APIKeys[1].APIKey.Deleted)
	require.Len(t, stats.Models, 1)
	require.Equal(t, "public-alias", stats.Models[0].ModelID)
	require.Len(t, stats.Daily, 2)
	require.Zero(t, stats.Daily[1].Metrics.Requests)
	require.Nil(t, stats.Daily[1].Metrics.SuccessRate)
}

func TestPersonalScopeRejectsForeignKeysAndRevokedMembership(t *testing.T) {
	e := newPersonalTestEnv(t)
	foreign := e.client.Project.Create().SetName("Not a member").SaveX(e.setup)
	key := e.client.APIKey.Create().SetName("Own creator, foreign project").SetKey("foreign").
		SetProjectID(foreign.ID).SetUserID(e.user.ID).SetType(apikey.TypePersonal).SaveX(e.setup)
	for _, scope := range []PersonalScope{
		{ProjectID: lo.ToPtr(objects.GUID{Type: ent.TypeProject, ID: foreign.ID})},
		{APIKeyID: lo.ToPtr(objects.GUID{Type: ent.TypeAPIKey, ID: key.ID})},
		{ProjectID: lo.ToPtr(objects.GUID{Type: ent.TypeUser, ID: e.user.ID})},
		{APIKeyID: lo.ToPtr(objects.GUID{Type: ent.TypeProject, ID: e.project.ID})},
	} {
		_, err := e.svc.personalKeys(e.ctx, scope)
		require.Error(t, err)
	}
	// Even a stale owner flag in a session cannot bypass current DB membership.
	e.user.IsOwner = true
	_, err := e.svc.personalKeys(e.ctx, PersonalScope{ProjectID: lo.ToPtr(objects.GUID{Type: ent.TypeProject, ID: foreign.ID})})
	require.Error(t, err)
	e.user.IsOwner = false
	_, err = e.client.UserProject.Delete().Exec(e.setup)
	require.NoError(t, err)
	workspace, err := e.svc.Workspace(e.ctx, nil)
	require.NoError(t, err)
	require.Empty(t, workspace.APIKeys)
	stats, err := e.svc.Dashboard(e.ctx, PersonalUsageInput{StartDate: "2026-09-10", EndDate: "2026-09-11"})
	require.NoError(t, err)
	require.Zero(t, stats.Overview.Requests)
	require.Empty(t, stats.APIKeys)
	// API-key principals cannot use a user's self-service API, even with a user attached.
	keyCtx := contexts.WithUser(authz.NewAPIKeyContext(context.Background(), e.key.ID, e.project.ID), e.user)
	_, err = e.svc.Workspace(keyCtx, nil)
	require.Error(t, err)
	_, err = e.svc.Workspace(context.Background(), nil)
	require.Error(t, err)
}

func TestPersonalCalendarBoundaries(t *testing.T) {
	for _, zone := range []string{"Asia/Kathmandu", "America/New_York"} {
		t.Run(zone, func(t *testing.T) {
			e := newPersonalTestEnv(t)
			loc, err := time.LoadLocation(zone)
			require.NoError(t, err)
			e.svc.system.timeLocation = loc
			start := time.Date(2026, 3, 8, 0, 0, 0, 0, loc)
			end := start.AddDate(0, 0, 1)
			e.addRequest(e.key, request.StatusCompleted, start.Add(-time.Second))
			e.addRequest(e.key, request.StatusCompleted, start)
			e.addRequest(e.key, request.StatusFailed, end.Add(-time.Second))
			e.addRequest(e.key, request.StatusCompleted, end)
			stats, err := e.svc.Dashboard(e.ctx, PersonalUsageInput{StartDate: "2026-03-08", EndDate: "2026-03-08"})
			require.NoError(t, err)
			require.Equal(t, float64(2), stats.Overview.Requests)
			require.Equal(t, stats.Overview.Requests, stats.Daily[0].Metrics.Requests)
		})
	}
	for _, dates := range [][2]string{{"bad", "2026-03-08"}, {"2026-02-30", "2026-03-08"}, {"2026-03-08", "2026-03-07"}, {"2026-01-01", "2026-12-31"}} {
		_, err := personalDateRange(dates[0], dates[1], time.UTC)
		require.Error(t, err)
	}
}

func TestPersonalModelsUseEffectiveKeyPermissions(t *testing.T) {
	e := newPersonalTestEnv(t)
	ch := e.client.Channel.Create().SetType(channel.TypeOpenai).SetName("Private channel").
		SetBaseURL("https://example.invalid/v1").SetCredentials(objects.ChannelCredentials{APIKey: "provider-secret"}).
		SetSupportedModels([]string{"allowed", "excluded"}).SetDefaultTestModel("allowed").SetStatus(channel.StatusEnabled).SaveX(e.setup)
	reloadEnabledChannels(t, e.setup, e.client, e.channels)
	for _, id := range []string{"allowed", "excluded"} {
		createConfiguredModel(t, e.setup, e.client, id, model.TypeChat, &objects.ModelAssociation{
			Type: "channel_model", ChannelModel: &objects.ChannelModelAssociation{ChannelID: ch.ID, ModelID: id},
		})
	}
	profiles := &objects.APIKeyProfiles{ActiveProfile: "personal", Profiles: []objects.APIKeyProfile{{
		Name: "personal", ChannelIDs: []int{ch.ID}, ModelIDs: []string{"allowed"},
	}}}
	e.client.APIKey.UpdateOne(e.key).SetProfiles(profiles).SaveX(e.setup)
	require.NoError(t, e.svc.system.SetModelSettings(e.setup, SystemModelSettings{QueryAllChannelModels: true}))
	result, err := e.svc.Models(e.ctx, PersonalScope{}, "")
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Equal(t, "allowed", result[0].ModelID)
	require.Len(t, result[0].APIKeys, 1)
	require.Equal(t, e.key.ID, result[0].APIKeys[0].ID.ID)
	_, hasAPIKey := contexts.GetAPIKey(e.ctx)
	require.False(t, hasAPIKey, "listing models must not mutate the caller context")
	principal, _ := authz.GetPrincipal(e.ctx)
	require.True(t, principal.IsUser())
	result, err = e.svc.Models(e.ctx, PersonalScope{}, "excluded")
	require.NoError(t, err)
	require.Empty(t, result)
	e.client.APIKey.UpdateOneID(e.key.ID).SetStatus(apikey.StatusDisabled).SaveX(e.setup)
	result, err = e.svc.Models(e.ctx, PersonalScope{}, "")
	require.NoError(t, err)
	require.Empty(t, result)
	e.client.User.UpdateOneID(e.user.ID).SetStatus(user.StatusDeactivated).SaveX(e.setup)
	_, err = e.svc.Workspace(e.ctx, nil)
	require.Error(t, err)
}

func TestPersonalDateExpressionUsesBoundParameters(t *testing.T) {
	days, err := personalDateRange("2026-03-08", "2026-03-09", time.UTC)
	require.NoError(t, err)
	query, args := personalDayExpression("created_at", days).Query()
	require.Contains(t, query, "CASE WHEN created_at < ? THEN ?")
	require.NotContains(t, query, "2026-03")
	require.Len(t, args, 4)
	require.Equal(t, "2026-03-08", fmt.Sprint(args[1]))
}

func TestPersonalScopeExcludesArchivedAndDeletedProjects(t *testing.T) {
	for _, state := range []string{"archived", "deleted"} {
		t.Run(state, func(t *testing.T) {
			e := newPersonalTestEnv(t)
			// Owners also retain only keys in active, non-deleted projects.
			e.client.User.UpdateOneID(e.user.ID).SetIsOwner(true).SaveX(e.setup)
			if state == "archived" {
				e.client.Project.UpdateOneID(e.project.ID).SetStatus(project.StatusArchived).SaveX(e.setup)
			} else {
				require.NoError(t, e.client.Project.DeleteOneID(e.project.ID).Exec(e.setup))
			}
			keys, err := e.svc.personalKeys(e.ctx, PersonalScope{})
			require.NoError(t, err)
			require.Empty(t, keys)
			_, err = e.svc.personalKeys(e.ctx, PersonalScope{APIKeyID: lo.ToPtr(objects.GUID{Type: ent.TypeAPIKey, ID: e.key.ID})})
			require.Error(t, err)
		})
	}
}

func TestPersonalModelsDoNotResurrectConfiguredIDsOutsideProjectChannels(t *testing.T) {
	e := newPersonalTestEnv(t)
	createChannel := func(name string) *ent.Channel {
		return e.client.Channel.Create().SetType(channel.TypeOpenai).SetName(name).
			SetBaseURL("https://example.invalid/v1").SetCredentials(objects.ChannelCredentials{APIKey: "secret"}).
			SetSupportedModels([]string{"collision"}).SetDefaultTestModel("collision").SetStatus(channel.StatusEnabled).SaveX(e.setup)
	}
	allowed, excluded := createChannel("Allowed"), createChannel("Excluded")
	e.client.Project.UpdateOne(e.project).SetProfiles(&objects.ProjectProfiles{
		ActiveProfile: "restricted", Profiles: []objects.ProjectProfile{{Name: "restricted", ChannelIDs: []int{allowed.ID}}},
	}).SaveX(e.setup)
	createConfiguredModel(t, e.setup, e.client, "collision", model.TypeChat, &objects.ModelAssociation{
		Type: "channel_model", ChannelModel: &objects.ChannelModelAssociation{ChannelID: excluded.ID, ModelID: "collision"},
	})
	reloadEnabledChannels(t, e.setup, e.client, e.channels)
	require.NoError(t, e.svc.system.SetModelSettings(e.setup, SystemModelSettings{QueryAllChannelModels: true}))
	models, err := e.svc.Models(e.ctx, PersonalScope{}, "")
	require.NoError(t, err)
	require.Empty(t, models, "a raw channel ID cannot restore a configured model with no accessible association")
}

func TestPersonalQuotaUsesActiveProfileWindowIndependentOfDateFilter(t *testing.T) {
	e := newPersonalTestEnv(t)
	quota := &objects.APIKeyQuota{Requests: lo.ToPtr(int64(10)), TotalTokens: lo.ToPtr(int64(5_000_000_000)),
		Period: objects.APIKeyQuotaPeriod{Type: objects.APIKeyQuotaPeriodTypeAllTime}}
	e.client.APIKey.UpdateOne(e.key).SetProfiles(&objects.APIKeyProfiles{
		ActiveProfile: "limited", Profiles: []objects.APIKeyProfile{{Name: "unused"}, {Name: "limited", Quota: quota}},
	}).SaveX(e.setup)
	at := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	r := e.addRequest(e.key, request.StatusCompleted, at)
	e.client.UsageLog.Create().SetRequestID(r.ID).SetAPIKeyID(e.key.ID).SetProjectID(e.project.ID).
		SetModelID("public-alias").SetPromptTokens(3_000_000_000).SetTotalTokens(3_000_000_000).SetTotalCost(1.25).SetCreatedAt(at).SaveX(e.setup)
	stats, err := e.svc.Dashboard(e.ctx, PersonalUsageInput{StartDate: "2026-01-01", EndDate: "2026-01-01"})
	require.NoError(t, err)
	require.Zero(t, stats.Overview.Requests)
	actual := stats.APIKeys[0].Quota
	require.NotNil(t, actual)
	require.Equal(t, "limited", actual.ProfileName)
	require.Equal(t, float64(1), actual.Requests)
	require.Equal(t, float64(3_000_000_000), actual.Tokens)
	require.Equal(t, 1.25, actual.Cost)
	require.Equal(t, int64(5_000_000_000), *actual.Limit.TotalTokens)
}
