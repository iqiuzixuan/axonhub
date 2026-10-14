package biz

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func testCPASettings(base string) *objects.ChannelSettings {
	return &objects.ChannelSettings{ProviderQuota: &objects.ChannelProviderQuotaSettings{CPA: &objects.CPAQuotaSettings{ManagementURL: base, APIVersion: objects.CPAManagementAPIVersionV8}}}
}

func TestCPA_ChannelLifecycleAndQuotaPersistence(t *testing.T) {
	channelService, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		require.Equal(t, "Bearer management-only", r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, "/credentials") {
			fmt.Fprint(w, `{"files":[{"provider":"codex","auth_index":"account-one"}]}`)
			return
		}
		fmt.Fprint(w, `{"status_code":200,"body":"{\"plan_type\":\"pro\",\"rate_limit\":{\"allowed\":true,\"primary_window\":{\"used_percent\":11,\"limit_window_seconds\":604800,\"reset_after_seconds\":3600}}}"}`)
	}))
	defer server.Close()
	systemService := NewSystemService(SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	quotaService := NewProviderQuotaService(ProviderQuotaServiceParams{Ent: client, SystemService: systemService, HttpClient: httpclient.NewHttpClientWithClient(server.Client())})
	channelService.SetChannelProviderQuotaInvalidator(quotaService)
	created, err := channelService.CreateChannel(ctx, ent.CreateChannelInput{Type: channel.TypeCodex, BaseURL: lo.ToPtr(server.URL + "/backend-api"), Name: "CPA fixture", Settings: testCPASettings(server.URL), Credentials: objects.ChannelCredentials{APIKey: "inference-only", ManagementAPIKey: "management-only"}, SupportedModels: []string{"test-model"}, DefaultTestModel: "test-model"})
	require.NoError(t, err)
	require.True(t, hasCredentialsForProvider(created))
	quotaService.checkChannelQuota(ctx, quotaCheckGroup{channels: []*ent.Channel{created}}, time.Now())
	persisted, err := client.ProviderQuotaStatus.Query().Where(providerquotastatus.ChannelIDEQ(created.ID)).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, providerquotastatus.StatusAvailable, persisted.Status)
	require.Equal(t, "cpa", persisted.QuotaData["quota_source"])
	require.Len(t, quotaService.GetQuotaStatus(ctx, created.ID).Limits, 1)
	require.Equal(t, int32(2), calls.Load(), "CPA must not list/reset official reset credits")
	resets, err := quotaService.ListResets(ctx, created.ID)
	require.NoError(t, err)
	require.False(t, resets.Supported)
	require.ErrorIs(t, quotaService.ResetChannelQuotaNow(ctx, created.ID), provider_quota.ErrResetUnsupported)

	updated, err := channelService.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Credentials: &objects.ChannelCredentials{APIKey: "changed-inference"}})
	require.NoError(t, err)
	require.Equal(t, "management-only", updated.Credentials.ManagementAPIKey)
	require.NotNil(t, quotaService.GetQuotaStatus(ctx, created.ID), "inference key changes do not change the CPA account pool")
	copySettings := testCPASettings(server.URL)
	copySettings.ProviderQuota.CPA.AuthIndex = "account-one"
	updated, err = channelService.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Settings: copySettings})
	require.NoError(t, err)
	require.Nil(t, quotaService.GetQuotaStatus(ctx, created.ID))
	count, err := client.ProviderQuotaStatus.Query().Where(providerquotastatus.ChannelIDEQ(created.ID)).Count(ctx)
	require.NoError(t, err)
	require.Zero(t, count)
	duplicated, err := channelService.DuplicateChannel(ctx, created.ID, ent.CreateChannelInput{Type: channel.TypeCodex, BaseURL: lo.ToPtr(server.URL + "/backend-api"), Name: "CPA duplicate", Settings: copySettings, Credentials: objects.ChannelCredentials{APIKey: "other-inference"}, SupportedModels: []string{"test-model"}, DefaultTestModel: "test-model"})
	require.NoError(t, err)
	require.Equal(t, "management-only", duplicated.Credentials.ManagementAPIKey)
	require.NotNil(t, duplicated.Settings.ProviderQuota.CPA)
	updated, err = channelService.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Settings: &objects.ChannelSettings{}})
	require.NoError(t, err)
	require.Empty(t, updated.Credentials.ManagementAPIKey)
	require.False(t, hasCredentialsForProvider(updated))
	require.Nil(t, updated.Settings.ProviderQuota)
}

func TestCPA_SettingsValidationAndIdentityChanges(t *testing.T) {
	settings := testCPASettings("http://cpa.example/v8/management/")
	normalized, err := normalizeCPAQuotaSettings(settings, channel.TypeCodex)
	require.NoError(t, err)
	require.Equal(t, "http://cpa.example", normalized.ProviderQuota.CPA.ManagementURL)
	cleared, err := normalizeCPAQuotaSettings(settings, channel.TypeOpenai)
	require.NoError(t, err)
	require.Nil(t, cleared.ProviderQuota)
	require.NotNil(t, settings.ProviderQuota.CPA, "normalization does not mutate the supplied settings")
	_, err = normalizeCPAQuotaSettings(testCPASettings("file:///secret"), channel.TypeCodex)
	require.Error(t, err)
	before := &ent.Channel{Type: channel.TypeCodex, BaseURL: "http://cpa.example/backend-api", Settings: normalized, Credentials: objects.ChannelCredentials{APIKey: "infer", ManagementAPIKey: "manage"}}
	for _, test := range []struct {
		name   string
		change func(*ent.Channel)
	}{
		{"url", func(ch *ent.Channel) { ch.Settings.ProviderQuota.CPA.ManagementURL = "http://other.example" }},
		{"version", func(ch *ent.Channel) { ch.Settings.ProviderQuota.CPA.APIVersion = objects.CPAManagementAPIVersionV0 }},
		{"account", func(ch *ent.Channel) { ch.Settings.ProviderQuota.CPA.AuthIndex = "other" }},
		{"management-key", func(ch *ent.Channel) { ch.Credentials.ManagementAPIKey = "other" }},
		{"oauth", func(ch *ent.Channel) { ch.Credentials.OAuth = &objects.OAuthCredentials{AccessToken: "oauth"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			after := *before
			after.Settings = testCPASettings(normalized.ProviderQuota.CPA.ManagementURL)
			test.change(&after)
			require.True(t, cpaQuotaIdentityChanged(before, &after))
		})
	}
	ordinary := &ent.Channel{Type: channel.TypeCodex, Credentials: objects.ChannelCredentials{APIKey: "plain-cpa-inference-key"}}
	require.False(t, hasCredentialsForProvider(ordinary), "a third-party key alone must not opt into CPA")
}
