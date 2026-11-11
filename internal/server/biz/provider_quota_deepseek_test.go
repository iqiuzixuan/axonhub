package biz

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz/provider_quota"
	"github.com/looplj/axonhub/llm/httpclient"
)

type deepSeekBalanceTransport func(*http.Request) (*http.Response, error)

func (f deepSeekBalanceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestProviderQuotaService_DeepSeekUpstreamBalancesAndCollectionSettings(t *testing.T) {
	service, system, ctx, client := setupProviderQuotaCollectionService(t)
	defer client.Close()
	ch := createProviderQuotaCollectionChannel(t, ctx, client, "DeepSeek", channel.TypeDeepseekAnthropic)
	var calls atomic.Int32
	amount, currency, responseStatus := "100", "CNY", http.StatusOK
	httpClient := httpclient.NewHttpClientWithClient(&http.Client{Transport: deepSeekBalanceTransport(func(req *http.Request) (*http.Response, error) {
		calls.Add(1)
		require.Equal(t, "https://api.deepseek.com/user/balance", req.URL.String())
		body := fmt.Sprintf(`{"is_available":%t,"balance_infos":[{"currency":%q,"total_balance":%q}]}`, amount != "0", currency, amount)
		return &http.Response{StatusCode: responseStatus, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
	})})
	service.httpClient = httpClient
	service.registerProviderQuotaSupport()
	check := func() { service.runQuotaCheckForce(ctx) }
	readStatus := func() *ent.ProviderQuotaStatus {
		status, err := client.ProviderQuotaStatus.Query().Where(providerquotastatus.ChannelIDEQ(ch.ID)).Only(ctx)
		require.NoError(t, err)
		return status
	}
	total := func() float64 {
		return readStatus().QuotaData["_balances"].([]any)[0].(map[string]any)["total"].(float64)
	}
	check()
	require.Positive(t, calls.Load())
	require.Equal(t, 100.0, total())
	amount = "60"
	check()
	require.Equal(t, 60.0, total())
	require.NotContains(t, readStatus().QuotaData["_balances"].([]any)[0].(map[string]any), "baseline_balance")
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, nil, []ProviderQuotaCollectionProvider{{Provider: "deepseek", Enabled: false}}))
	previousCalls := calls.Load()
	check()
	require.Equal(t, previousCalls, calls.Load())
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, nil, []ProviderQuotaCollectionProvider{{Provider: "deepseek", Enabled: true}}))
	amount = "0"
	check()
	require.Equal(t, providerquotastatus.StatusExhausted, readStatus().Status)
	amount = "20"
	check()
	require.Equal(t, providerquotastatus.StatusAvailable, readStatus().Status)
	require.Equal(t, 20.0, total())
	require.Empty(t, service.GetQuotaStatus(ctx, ch.ID).Limits)

}

func TestChannelService_DeepSeekKeyChangeInvalidatesBalanceImmediately(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	quotaService := &ProviderQuotaService{AbstractService: &AbstractService{db: client}}
	svc.SetChannelProviderQuotaInvalidator(quotaService)
	ch := createProviderQuotaCollectionChannel(t, ctx, client, "DeepSeek", channel.TypeDeepseek)
	quotaService.saveQuotaStatus(ctx, ch, "deepseek", "", provider_quota.QuotaData{Status: "available", Ready: true, RawData: map[string]any{"balance_infos": []any{}}}, ch.CreatedAt, false)

	_, err := svc.UpdateChannel(ctx, ch.ID, &ent.UpdateChannelInput{Credentials: &objects.ChannelCredentials{APIKey: "test-key"}})
	require.NoError(t, err)
	exists, err := client.ProviderQuotaStatus.Query().Where(providerquotastatus.ChannelIDEQ(ch.ID)).Exist(ctx)
	require.NoError(t, err)
	require.True(t, exists, "saving the same key must preserve the baseline")

	_, err = svc.UpdateChannel(ctx, ch.ID, &ent.UpdateChannelInput{Credentials: &objects.ChannelCredentials{APIKey: "replacement-key"}})
	require.NoError(t, err)
	exists, err = client.ProviderQuotaStatus.Query().Where(providerquotastatus.ChannelIDEQ(ch.ID)).Exist(ctx)
	require.NoError(t, err)
	require.False(t, exists)
	_, cached := quotaService.quotaCache.Load(ch.ID)
	require.False(t, cached)
}
