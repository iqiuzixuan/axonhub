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

func TestProviderQuotaService_DeepSeekOptInPersistenceAndRecovery(t *testing.T) {
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
	baseline := func(status *ent.ProviderQuotaStatus) string {
		return status.QuotaData["balance_infos"].([]any)[0].(map[string]any)["baseline_balance"].(string)
	}

	check()
	require.Zero(t, calls.Load(), "default-off must prevent even manual polling")
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, nil, []ProviderQuotaCollectionProvider{{Provider: "deepseek", Enabled: true}}))
	check()
	require.Equal(t, "100", baseline(readStatus()))
	amount = "60"
	check()
	require.Equal(t, "100", baseline(readStatus()))

	// Reconstructing the service proves the reference comes from SQL, not memory.
	service = NewProviderQuotaService(ProviderQuotaServiceParams{Ent: client, SystemService: system, HttpClient: httpClient})
	amount = "50"
	check()
	require.Equal(t, "100", baseline(readStatus()))
	for _, failedStatus := range []int{500, 401} {
		responseStatus = failedStatus
		check()
		failed := readStatus()
		require.Equal(t, "100", baseline(failed))
		require.Equal(t, providerquotastatus.StatusUnknown, failed.Status)
		require.False(t, failed.Ready)
		require.Equal(t, "check_failed", failed.QuotaData["error_code"])
		responseStatus, amount = 200, "40"
		check()
		require.Equal(t, "100", baseline(readStatus()))
		require.NotContains(t, readStatus().QuotaData, "error_code")
	}

	// Switching collection off preserves the reference but hides routing state.
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, nil, []ProviderQuotaCollectionProvider{{Provider: "deepseek", Enabled: false}}))
	previousCalls := calls.Load()
	check()
	require.Equal(t, previousCalls, calls.Load())
	require.Nil(t, service.GetQuotaStatus(ctx, ch.ID))
	require.NoError(t, system.UpdateProviderQuotaCollectionSettings(ctx, nil, []ProviderQuotaCollectionProvider{{Provider: "deepseek", Enabled: true}}))
	amount = "30"
	check()
	require.Equal(t, "100", baseline(readStatus()))

	for _, step := range []struct{ amount, baseline string }{{"200", "200"}, {"50", "200"}, {"0", "0"}, {"150", "150"}} {
		amount = step.amount
		check()
		require.Equal(t, step.baseline, baseline(readStatus()))
	}
	require.Empty(t, service.GetQuotaStatus(ctx, ch.ID).Limits, "balance references are display-only")

	_, err := client.Channel.UpdateOneID(ch.ID).SetCredentials(objects.ChannelCredentials{APIKey: "new-test-key"}).Save(ctx)
	require.NoError(t, err)
	amount = "20"
	check()
	require.Equal(t, "20", baseline(readStatus()), "a new credential must not inherit the old baseline")
	currency, amount = "USD", "10"
	check()
	require.Equal(t, "10", baseline(readStatus()))
}

func TestChannelService_DeepSeekKeyChangeInvalidatesBalanceImmediately(t *testing.T) {
	svc, client := setupTestChannelService(t)
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	quotaService := &ProviderQuotaService{AbstractService: &AbstractService{db: client}}
	svc.SetChannelProviderQuotaInvalidator(quotaService)
	ch := createProviderQuotaCollectionChannel(t, ctx, client, "DeepSeek", channel.TypeDeepseek)
	quotaService.saveQuotaStatus(ctx, ch.ID, "deepseek", "", provider_quota.QuotaData{Status: "available", Ready: true, RawData: map[string]any{"balance_infos": []any{}}}, ch.CreatedAt)

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
