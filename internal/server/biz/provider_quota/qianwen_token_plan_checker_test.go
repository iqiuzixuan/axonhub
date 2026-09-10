package provider_quota

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

// This models the public console envelope, not a captured user account.
const qianwenUsageFixture = `{"code":"200","data":{"success":true,"DataV2":{"ret":["SUCCESS::success"],"data":{"data":{"per1WeekPercentage":0.25,"per1WeekResetTime":"2099-09-17T12:00:00Z"}}}}}`

func TestQianwenTokenPlanQuotaChecker_ConsoleRequests(t *testing.T) {
	for _, typ := range []channel.Type{channel.TypeQianwenTokenPlan, channel.TypeQianwenTokenPlanAnthropic} {
		t.Run(string(typ), func(t *testing.T) {
			requests := 0
			client := httpclient.NewHttpClientWithClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				requests++
				require.Equal(t, "session=fixture-cookie", req.Header.Get("Cookie"))
				require.Empty(t, req.Header.Get("Authorization"))
				body := qianwenUsageFixture
				if requests == 1 {
					require.Equal(t, http.MethodGet, req.Method)
					require.Equal(t, qianwenUserInfoURL, req.URL.String())
					body = `{"code":"200","data":{"secToken":"fixture-csrf"}}`
				} else {
					require.Equal(t, http.MethodPost, req.Method)
					require.Equal(t, "cs-data.qianwenai.com", req.URL.Host)
					require.Equal(t, qianwenPersonalUsageAPI, req.URL.Query().Get("api"))
					require.NoError(t, req.ParseForm())
					require.Equal(t, "fixture-csrf", req.PostForm.Get("sec_token"))
					require.Equal(t, "sfm_bailian", req.PostForm.Get("product"))
					require.Equal(t, "BroadScopeAspnGateway", req.PostForm.Get("action"))
					require.Equal(t, "cn-beijing", req.PostForm.Get("region"))
					var params map[string]any
					require.NoError(t, json.Unmarshal([]byte(req.PostForm.Get("params")), &params))
					require.Equal(t, qianwenPersonalUsageAPI, params["Api"])
					require.Equal(t, "1.0", params["V"])
					data := params["Data"].(map[string]any)
					require.Equal(t, "QIANWENAI", data["cornerstoneParam"].(map[string]any)["consoleSite"])
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			})})
			checker := NewQianwenTokenPlanQuotaChecker(client)
			ch := &ent.Channel{Type: typ, BaseURL: "https://unrelated.example/v1", Credentials: objects.ChannelCredentials{APIKey: "sk-sp-inference-only"},
				Settings: &objects.ChannelSettings{ProviderQuota: &objects.ChannelProviderQuotaSettings{
					QianwenTokenPlan: &objects.QianwenTokenPlanQuotaSettings{AuthCookie: "Cookie: session=fixture-cookie"},
				}}}
			quota, err := checker.CheckQuota(context.Background(), ch)
			require.NoError(t, err)
			require.Equal(t, 2, requests)
			require.True(t, checker.SupportsChannel(ch))
			require.Equal(t, "qianwen_token_plan", quota.ProviderType)
			require.Len(t, quota.Limits, 1)
			require.Equal(t, "7d", quota.Limits[0].Window)
			require.InDelta(t, 0.25, quota.Limits[0].UsageRatio, 1e-9)
			require.Equal(t, "2099-09-10T12:00:00Z", quota.Limits[0].PeriodStart.UTC().Format(time.RFC3339))
			require.Equal(t, float64(75), quota.RawData["remaining_percent"])
			serialized, err := json.Marshal(quota)
			require.NoError(t, err)
			require.NotContains(t, string(serialized), "fixture-cookie")
			require.NotContains(t, string(serialized), "fixture-csrf")
			assertNormalizedLimitContract(t, quota)
		})
	}
}

func TestQianwenQuotaParser_ReportedUsage(t *testing.T) {
	for _, tc := range []struct {
		value  string
		status string
	}{{"0", "available"}, {"0.85", "warning"}, {"1", "exhausted"}, {"1.1", "exhausted"}} {
		t.Run(tc.value, func(t *testing.T) {
			quota, err := parseQianwenTokenPlanQuotaResponse([]byte(strings.Replace(qianwenUsageFixture, "0.25", tc.value, 1)))
			require.NoError(t, err)
			require.Equal(t, tc.status, quota.Status)
			require.Equal(t, IsReadyStatus(tc.status), quota.Ready)
		})
	}
	// The console also accepts DataV2.data without the final data wrapper.
	quota, err := parseQianwenTokenPlanQuotaResponse([]byte(`{"successResponse":true,"data":{"DataV2":{"data":{"per1WeekPercentage":0.4}}}}`))
	require.NoError(t, err)
	require.Nil(t, quota.NextResetAt)
	require.Nil(t, quota.Limits[0].PeriodStart)
}

func TestQianwenQuotaParser_RejectsUnknownUsageAndErrors(t *testing.T) {
	for _, body := range []string{
		`{}`, `<html>sign in</html>`,
		`{"code":"200","data":{"success":false}}`,
		`{"code":"200","data":{"DataV2":{"data":{"per1WeekResetTime":123}}}}`,
		`{"code":"200","data":{"DataV2":{"data":{"success":false,"per1WeekPercentage":0.2}}}}`,
		strings.Replace(qianwenUsageFixture, "0.25", "null", 1),
		strings.Replace(qianwenUsageFixture, "0.25", "-1", 1),
		strings.Replace(qianwenUsageFixture, "SUCCESS::success", "FAIL::fixture-sensitive", 1),
	} {
		quota, err := parseQianwenTokenPlanQuotaResponse([]byte(body))
		require.Error(t, err)
		require.Empty(t, quota.Limits)
		require.NotContains(t, err.Error(), "fixture-sensitive")
	}
	for _, code := range []string{"ConsoleNeedLogin", "BailianGateway.Login.NotLogined", "NO_LOGIN", "401", "403"} {
		_, err := parseQianwenTokenPlanQuotaResponse([]byte(`{"code":"` + code + `"}`))
		require.ErrorIs(t, err, ErrInvalidCredentials)
	}
}

func TestQianwenQuotaReset_Formats(t *testing.T) {
	for _, raw := range []string{`"2099-09-17T12:00:00Z"`, `"2099-09-17 20:00:00"`, `4093329600000`, `"4093329600000"`} {
		got := parseQianwenQuotaReset(json.RawMessage(raw))
		require.NotNil(t, got)
		require.Equal(t, "2099-09-17T12:00:00Z", got.UTC().Format(time.RFC3339))
	}
	for _, raw := range []string{`null`, `""`, `0`, `"bad date"`} {
		require.Nil(t, parseQianwenQuotaReset(json.RawMessage(raw)))
	}
}

func TestQianwenQuotaChecker_InvalidSession(t *testing.T) {
	for _, status := range []int{200, 401, 403, 500} {
		client := httpclient.NewHttpClientWithClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":"ConsoleNeedLogin","message":"fixture-sensitive"}`))}, nil
		})})
		checker := NewQianwenTokenPlanQuotaChecker(client)
		_, err := checker.CheckQuota(context.Background(), &ent.Channel{Settings: &objects.ChannelSettings{ProviderQuota: &objects.ChannelProviderQuotaSettings{QianwenTokenPlan: &objects.QianwenTokenPlanQuotaSettings{AuthCookie: "session=fixture"}}}})
		require.Error(t, err)
		if status != 500 {
			require.ErrorIs(t, err, ErrInvalidCredentials)
		}
		require.NotContains(t, err.Error(), "fixture-sensitive")
	}
	_, err := NewQianwenTokenPlanQuotaChecker(nil).CheckQuota(context.Background(), &ent.Channel{Credentials: objects.ChannelCredentials{APIKey: "sk-sp-key"}})
	require.ErrorIs(t, err, ErrInvalidCredentials)
}
