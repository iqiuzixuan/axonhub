package biz

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
)

func TestQianwenQuotaSettings_PersistenceAndTypeChange(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	settings := &objects.ChannelSettings{QuotaRoutingMode: objects.QuotaRoutingMode("IGNORE_QUOTA"), ProviderQuota: &objects.ChannelProviderQuotaSettings{
		QianwenTokenPlan: &objects.QianwenTokenPlanQuotaSettings{AuthCookie: "Cookie: session=fixture-personal"},
	}}
	created, err := svc.createChannel(ctx, ent.CreateChannelInput{
		Type: channel.TypeQianwenTokenPlan, Name: "Qianwen personal quota",
		BaseURL:         lo.ToPtr("https://token-plan.cn-beijing.maas.aliyuncs.com/compatible-mode/v1"),
		Credentials:     objects.ChannelCredentials{APIKey: "sk-sp-inference"},
		SupportedModels: []string{"qwen3.8-max"}, DefaultTestModel: "qwen3.8-max", Settings: settings,
	})
	require.NoError(t, err)
	require.Equal(t, "session=fixture-personal", created.Settings.ProviderQuota.QianwenTokenPlan.AuthCookie)
	require.True(t, hasCredentialsForProvider(created))
	require.Contains(t, settings.ProviderQuota.QianwenTokenPlan.AuthCookie, "Cookie:", "caller input must remain unchanged")

	updated, err := svc.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Settings: &objects.ChannelSettings{
		ProviderQuota: &objects.ChannelProviderQuotaSettings{QianwenTokenPlan: &objects.QianwenTokenPlanQuotaSettings{AuthCookie: " "}},
	}})
	require.NoError(t, err)
	require.Nil(t, updated.Settings.ProviderQuota)
	require.False(t, hasCredentialsForProvider(updated), "inference key cannot authenticate quota reads")

	_, err = svc.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Settings: settings})
	require.NoError(t, err)
	updated, err = svc.UpdateChannel(ctx, created.ID, &ent.UpdateChannelInput{Type: lo.ToPtr(channel.TypeOpenai)})
	require.NoError(t, err)
	require.Nil(t, updated.Settings.ProviderQuota, "type-only changes clear the old console cookie")
	require.Equal(t, objects.QuotaRoutingMode("IGNORE_QUOTA"), updated.Settings.QuotaRoutingMode)
}

func TestQianwenQuotaSettings_RejectBadCookieAndUnrelatedProvider(t *testing.T) {
	settings := &objects.ChannelSettings{ProviderQuota: &objects.ChannelProviderQuotaSettings{
		QianwenTokenPlan: &objects.QianwenTokenPlanQuotaSettings{AuthCookie: "sk-sp-inference-key"},
	}}
	_, err := normalizeQianwenQuotaSettings(settings, channel.TypeQianwenTokenPlan)
	require.Error(t, err)
	clean, err := normalizeQianwenQuotaSettings(settings, channel.TypeBailian)
	require.NoError(t, err)
	require.Nil(t, clean.ProviderQuota)
	settings.ProviderQuota.QianwenTokenPlan.AuthCookie = "session=value\nAuthorization: secret"
	_, err = normalizeQianwenQuotaSettings(settings, channel.TypeQianwenTokenPlanAnthropic)
	require.Error(t, err)
}
