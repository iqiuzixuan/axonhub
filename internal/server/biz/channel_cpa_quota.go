package biz

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
)

func normalizeCPAQuotaSettings(settings *objects.ChannelSettings, typ channel.Type) (*objects.ChannelSettings, error) {
	if settings == nil || settings.ProviderQuota == nil || settings.ProviderQuota.CPA == nil {
		return settings, nil
	}
	copySettings, quota := *settings, *settings.ProviderQuota
	if typ != channel.TypeCodex {
		quota.CPA = nil
	} else {
		normalized, err := provider_quota.NormalizeCPASettings(*quota.CPA)
		if err != nil {
			return nil, fmt.Errorf("invalid CPA quota settings: %w", err)
		}
		quota.CPA = &normalized
	}
	copySettings.ProviderQuota = &quota
	if quota == (objects.ChannelProviderQuotaSettings{}) {
		copySettings.ProviderQuota = nil
	}
	return &copySettings, nil
}

func cpaSettingsPresent(settings, fallback *objects.ChannelSettings) bool {
	if settings == nil {
		settings = fallback
	}
	return settings != nil && settings.ProviderQuota != nil && settings.ProviderQuota.CPA != nil
}

func cpaQuotaIdentityChanged(before, after *ent.Channel) bool {
	old, current := provider_quota.CPASettings(before), provider_quota.CPASettings(after)
	if old == nil && current == nil {
		return false
	}
	return !reflect.DeepEqual(old, current) || before.Type != after.Type || before.BaseURL != after.BaseURL ||
		strings.TrimSpace(before.Credentials.ManagementAPIKey) != strings.TrimSpace(after.Credentials.ManagementAPIKey) ||
		before.Credentials.IsOAuth() != after.Credentials.IsOAuth()
}
