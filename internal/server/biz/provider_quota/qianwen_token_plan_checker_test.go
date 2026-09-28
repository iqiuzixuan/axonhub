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

func TestQianwenQuotaParser_MissingWeeklyResetIsNotInvented(t *testing.T) {
	// The live personal console can return only the usage field.
	// Subscription endTime is not a quota reset and cannot fill it.
	body := []byte(`{"code":"200","data":{"success":true,"DataV2":{"data":{"data":{"per1WeekPercentage":0.0}}}}}`)
	quota, err := parseQianwenTokenPlanQuotaResponse(body)
	require.NoError(t, err)
	require.Len(t, quota.Limits, 1)
	require.Zero(t, quota.Limits[0].UsageRatio)
	require.Nil(t, quota.NextResetAt)
	require.Nil(t, quota.Limits[0].NextResetAt)
	require.Nil(t, quota.Limits[0].PeriodStart)
	require.Nil(t, quota.Limits[0].PeriodQuota)
}

func TestQianwenQuotaParser_MonthlyUsage(t *testing.T) {
	// Sanitized shape returned by the personal plan endpoint on 2026-09-25.
	// Keep the reset in the future so normalization does not discard it.
	body := []byte(`{"code":"200","data":{"success":true,"DataV2":{"ret":["SUCCESS::接口调用成功"],"data":{"msg":"Success.","code":"SUCCESS","data":{"per1MonthPercentage":0.27052849091666664,"per1MonthResetTime":"2099-10-21T00:00:00+08:00"},"success":true}}},"successResponse":true}`)
	quota, err := parseQianwenTokenPlanQuotaResponse(body)
	require.NoError(t, err)
	require.Equal(t, "available", quota.Status)
	require.True(t, quota.Ready)
	require.Len(t, quota.Limits, 1)
	limit := quota.Limits[0]
	require.Equal(t, QuotaWindowMonthly, limit.Window)
	require.InDelta(t, 0.27052849091666664, limit.UsageRatio, 1e-12)
	require.InDelta(t, 72.94715090833334, quota.RawData["remaining_percent"], 1e-9)
	require.Equal(t, "2099-10-20T16:00:00Z", quota.NextResetAt.UTC().Format(time.RFC3339))
	require.Equal(t, "2099-09-20T16:00:00Z", limit.PeriodStart.UTC().Format(time.RFC3339))
	assertNormalizedLimitContract(t, quota)
}

func TestQianwenQuotaParser_MonthlyWindows(t *testing.T) {
	for _, tc := range []struct {
		name   string
		usage  string
		status string
		ratio  float64
	}{
		{"unused", `{"per1MonthPercentage":0}`, "available", 0},
		{"warning", `{"per1MonthPercentage":0.85}`, "warning", 0.85},
		{"exhausted", `{"per1MonthPercentage":1}`, "exhausted", 1},
		{"over limit", `{"per1MonthPercentage":1.1}`, "exhausted", 1},
		{"monthly exhausted with weekly available", `{"per1MonthPercentage":1,"per1WeekPercentage":0.2}`, "exhausted", 1},
		{"weekly exhausted with monthly available", `{"per1MonthPercentage":0.2,"per1WeekPercentage":1}`, "exhausted", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			quota, err := parseQianwenTokenPlanQuotaResponse([]byte(`{"successResponse":true,"data":{"DataV2":{"data":` + tc.usage + `}}}`))
			require.NoError(t, err)
			require.Equal(t, tc.status, quota.Status)
			require.Equal(t, IsReadyStatus(tc.status), quota.Ready)
			require.InDelta(t, tc.ratio*100, quota.RawData["used_percent"], 1e-9)
			require.Nil(t, quota.NextResetAt)
			for _, limit := range quota.Limits {
				require.Nil(t, limit.PeriodStart)
				require.Nil(t, limit.PeriodQuota)
			}
			if strings.Contains(tc.usage, "per1WeekPercentage") {
				require.Len(t, quota.Limits, 2)
				require.Equal(t, QuotaWindow7d, quota.Limits[0].Window)
			}
			require.Equal(t, QuotaWindowMonthly, quota.Limits[len(quota.Limits)-1].Window)
			assertNormalizedLimitContract(t, quota)
		})
	}
}

func TestQianwenQuotaParser_MonthlyTimestampUsesConsoleTimeZone(t *testing.T) {
	// March 1 in China is still February in UTC. Do not subtract a month in UTC
	// or treat a monthly window as a fixed 30 days.
	reset := time.Date(2099, time.March, 1, 0, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*60*60))
	body, err := json.Marshal(map[string]any{"successResponse": true, "data": map[string]any{"DataV2": map[string]any{"data": map[string]any{
		"per1MonthPercentage": 0.2, "per1MonthResetTime": reset.UnixMilli(),
		"per1WeekPercentage": 0.4, "per1WeekResetTime": reset.Add(-24 * time.Hour).UnixMilli(),
	}}}})
	require.NoError(t, err)
	quota, err := parseQianwenTokenPlanQuotaResponse(body)
	require.NoError(t, err)
	require.Len(t, quota.Limits, 2)
	require.Equal(t, "2099-01-31T16:00:00Z", quota.Limits[1].PeriodStart.UTC().Format(time.RFC3339))
	require.True(t, quota.NextResetAt.Equal(reset.Add(-24*time.Hour)))
}

func TestQianwenQuotaParser_RejectsUnknownUsageAndErrors(t *testing.T) {
	for _, body := range []string{
		`{}`, `<html>sign in</html>`,
		`{"code":"200","data":{"success":false}}`,
		`{"code":"200","data":{"DataV2":{"data":{"per1WeekResetTime":123}}}}`,
		`{"code":"200","data":{"DataV2":{"data":{"success":false,"per1WeekPercentage":0.2}}}}`,
		strings.Replace(qianwenUsageFixture, "0.25", "null", 1),
		strings.Replace(qianwenUsageFixture, "0.25", "-1", 1),
		`{"code":"200","data":{"DataV2":{"data":{"per1MonthResetTime":123}}}}`,
		`{"code":"200","data":{"DataV2":{"data":{"per1MonthPercentage":null}}}}`,
		`{"code":"200","data":{"DataV2":{"data":{"per1MonthPercentage":-0.1,"per1WeekPercentage":0.2}}}}`,
		`{"code":"200","data":{"DataV2":{"data":{"per1MonthPercentage":"invalid"}}}}`,
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
