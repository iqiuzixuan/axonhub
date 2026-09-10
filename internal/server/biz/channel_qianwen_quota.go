package biz

import (
	"strings"

	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
)

func isQianwenTokenPlanChannelType(typ channel.Type) bool {
	return typ == channel.TypeQianwenTokenPlan || typ == channel.TypeQianwenTokenPlanAnthropic
}

func normalizeQianwenQuotaSettings(settings *objects.ChannelSettings, typ channel.Type) (*objects.ChannelSettings, error) {
	if settings == nil || settings.ProviderQuota == nil || settings.ProviderQuota.QianwenTokenPlan == nil {
		return settings, nil
	}
	copySettings := *settings
	quota := *settings.ProviderQuota
	copySettings.ProviderQuota = &quota
	cookie := strings.TrimSpace(quota.QianwenTokenPlan.AuthCookie)
	if !isQianwenTokenPlanChannelType(typ) || cookie == "" {
		quota.QianwenTokenPlan = nil
		if quota == (objects.ChannelProviderQuotaSettings{}) {
			copySettings.ProviderQuota = nil
		}
		return &copySettings, nil
	}
	normalized, err := provider_quota.NormalizeQianwenQuotaCookie(cookie)
	if err != nil {
		return nil, err
	}
	quota.QianwenTokenPlan = &objects.QianwenTokenPlanQuotaSettings{AuthCookie: normalized}
	return &copySettings, nil
}
