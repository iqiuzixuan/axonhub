package provider_quota

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

func deepSeekTestBody(amount, currency string, available bool) []byte {
	return fmt.Appendf(nil, `{"is_available":%t,"balance_infos":[{"currency":%q,"total_balance":%q,"granted_balance":"10","topped_up_balance":"90"}]}`, available, currency, amount)
}

func deepSeekReloadData(t *testing.T, data map[string]any) map[string]any {
	t.Helper()
	encoded, err := json.Marshal(data)
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, json.Unmarshal(encoded, &persisted))
	return persisted
}

func TestDeepSeek_DynamicBaselineAcrossRechargeCycles(t *testing.T) {
	var previous map[string]any
	for _, step := range []struct{ amount, baseline string }{
		{"100", "100"}, {"60", "100"}, {"200", "200"}, {"50", "200"},
		{"0", "0"}, {"150", "150"}, {"75", "150"}, {"30", "150"}, {"80", "80"},
		{"80.00", "80"}, {"10", "80"}, {"20", "20"},
	} {
		quota, err := parseDeepSeekBalance(deepSeekTestBody(step.amount, "CNY", step.amount != "0"), "key-a", previous)
		require.NoError(t, err)
		balances := quota.RawData["balance_infos"].([]DeepSeekBalance)
		require.Equal(t, step.baseline, balances[0].BaselineBalance, "balance %s", step.amount)
		require.Empty(t, quota.Limits, "visual references must not become routing limits")
		require.Nil(t, quota.NextResetAt)
		if step.amount == "0" {
			require.Equal(t, "exhausted", quota.Status)
			require.False(t, quota.Ready)
		} else {
			require.Equal(t, "available", quota.Status)
		}
		previous = deepSeekReloadData(t, quota.RawData)
		require.NotContains(t, fmt.Sprint(previous), "granted_balance")
		require.NotContains(t, fmt.Sprint(previous), "topped_up_balance")
	}
}

func TestDeepSeek_BaselineResetsForNewKeyOrCurrency(t *testing.T) {
	first, err := parseDeepSeekBalance(deepSeekTestBody("100", "CNY", true), "key-a", nil)
	require.NoError(t, err)
	previous := deepSeekReloadData(t, first.RawData)
	for _, step := range []struct{ identity, currency string }{
		{"key-b", "CNY"}, {"key-a", "USD"},
	} {
		quota, err := parseDeepSeekBalance(deepSeekTestBody("40", step.currency, true), step.identity, previous)
		require.NoError(t, err)
		require.Equal(t, "40", quota.RawData["balance_infos"].([]DeepSeekBalance)[0].BaselineBalance)
	}
}

func TestDeepSeek_IncreasesUseExactDecimalComparison(t *testing.T) {
	quota, err := parseDeepSeekBalance(deepSeekTestBody("0.300000000000000001", "CNY", true), "key", nil)
	require.NoError(t, err)
	quota, err = parseDeepSeekBalance(deepSeekTestBody("0.300000000000000002", "CNY", true), "key", quota.RawData)
	require.NoError(t, err)
	require.Equal(t, "0.300000000000000002", quota.RawData["balance_infos"].([]DeepSeekBalance)[0].BaselineBalance)
}

func TestDeepSeek_MalformedResponsesDoNotFabricateZero(t *testing.T) {
	for _, body := range []string{
		`{}`, `null`, `{"is_available":true,"balance_infos":[]}`,
		`{"balance_infos":[{"currency":"CNY","total_balance":"100"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":null}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":100}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"EUR","total_balance":"100"}]}`,
		`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"1"},{"currency":"CNY","total_balance":"1"}]}`,
	} {
		_, err := parseDeepSeekBalance([]byte(body), "key", nil)
		require.Error(t, err, body)
	}
	for _, amount := range []string{"", "-1", "NaN", "Infinity", "1e999999", strings.Repeat("1", 65)} {
		_, err := parseDeepSeekBalance(deepSeekTestBody(amount, "CNY", true), "key", nil)
		require.Error(t, err, amount)
	}
}

func TestDeepSeek_MultipleCurrenciesRemainSeparate(t *testing.T) {
	quota, err := parseDeepSeekBalance([]byte(`{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"100"},{"currency":"USD","total_balance":"10"}]}`), "key", nil)
	require.NoError(t, err)
	quota, err = parseDeepSeekBalance([]byte(`{"is_available":true,"balance_infos":[{"currency":"USD","total_balance":"5"},{"currency":"CNY","total_balance":"0"}]}`), "key", deepSeekReloadData(t, quota.RawData))
	require.NoError(t, err)
	balances := quota.RawData["balance_infos"].([]DeepSeekBalance)
	require.Equal(t, "10", balances[0].BaselineBalance)
	require.Equal(t, "0", balances[1].BaselineBalance)
	require.True(t, quota.Ready)
}

func TestDeepSeek_CheckQuotaUsesOfficialEndpointForBothProtocols(t *testing.T) {
	client := httpclient.NewHttpClientWithClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			require.Equal(t, deepSeekBalanceURL, req.URL.String())
			require.Equal(t, http.MethodGet, req.Method)
			require.Equal(t, "Bearer fixture-key", req.Header.Get("Authorization"))
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(deepSeekTestBody("100", "CNY", true))))}, nil
		}),
	})
	checker := NewDeepSeekQuotaChecker(client)
	for _, channelType := range []channel.Type{channel.TypeDeepseek, channel.TypeDeepseekAnthropic} {
		for _, base := range []string{"", "https://api.deepseek.com/v1", "https://api.deepseek.com/anthropic"} {
			ch := &ent.Channel{Type: channelType, BaseURL: base, Credentials: objects.ChannelCredentials{APIKeys: []string{"", " fixture-key "}}}
			quota, err := checker.CheckQuota(t.Context(), ch)
			require.NoError(t, err)
			require.NotContains(t, fmt.Sprint(quota.RawData), "fixture-key")
			require.Equal(t, "deepseek", quota.ProviderType)
		}
	}
	for _, base := range []string{"https://other.example/v1", "https://api.deepseek.com.evil.example", "http://api.deepseek.com", "https://user@api.deepseek.com"} {
		_, err := checker.CheckQuota(t.Context(), &ent.Channel{Type: channel.TypeDeepseek, BaseURL: base, Credentials: objects.ChannelCredentials{APIKey: "fixture-key"}})
		require.ErrorContains(t, err, "official API channel")
	}
}

func TestDeepSeek_CheckQuotaErrorsDoNotExposeResponseBody(t *testing.T) {
	for _, status := range []int{401, 403, 429, 500} {
		client := httpclient.NewHttpClientWithClient(&http.Client{
			Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("echoed-secret"))}, nil
			}),
		})
		_, err := NewDeepSeekQuotaChecker(client).CheckQuota(t.Context(), &ent.Channel{Type: channel.TypeDeepseek, Credentials: objects.ChannelCredentials{APIKey: "fixture-key"}})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "echoed-secret")
		if status == 401 || status == 403 {
			require.ErrorIs(t, err, ErrInvalidCredentials)
		}
	}
}
