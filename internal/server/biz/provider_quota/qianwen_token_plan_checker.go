package provider_quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
)

const (
	qianwenUserInfoURL      = "https://platform-home.qianwenai.com/tool/user/info.json"
	qianwenGatewayURL       = "https://cs-data.qianwenai.com/data/api.json"
	qianwenPersonalUsageAPI = "zeldaHttp.apikeyMgr./tokenplan/personal/api/v2/usage"
)

// QianwenTokenPlanQuotaChecker reads the personal plan's seven-day window from
// the console API. The console cookie is separate from the inference API key.
// Protocol source: console-home/1.1.27/assets/{analytics,shared}.js on
// https://q.alyasset.com/code/qwen-cloud/ (verified 2026-09-10).
type QianwenTokenPlanQuotaChecker struct {
	httpClient *httpclient.HttpClient
}

func NewQianwenTokenPlanQuotaChecker(client *httpclient.HttpClient) *QianwenTokenPlanQuotaChecker {
	return &QianwenTokenPlanQuotaChecker{httpClient: client}
}

func (c *QianwenTokenPlanQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	return ch.Type == channel.TypeQianwenTokenPlan || ch.Type == channel.TypeQianwenTokenPlanAnthropic
}

// NormalizeQianwenQuotaCookie accepts a browser Cookie header, not cURL or an
// inference key. The cookie is sent only to the two fixed console endpoints.
func NormalizeQianwenQuotaCookie(raw string) (string, error) {
	cookie := strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(cookie), "cookie:") {
		cookie = strings.TrimSpace(cookie[len("cookie:"):])
	}
	if cookie == "" || strings.ContainsAny(cookie, "\r\n") || !strings.Contains(cookie, "=") {
		return "", fmt.Errorf("invalid Qianwen console Cookie header")
	}
	return cookie, nil
}

func (c *QianwenTokenPlanQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	if ch.Settings == nil || ch.Settings.ProviderQuota == nil || ch.Settings.ProviderQuota.QianwenTokenPlan == nil {
		return QuotaData{}, fmt.Errorf("%w: Qianwen console cookie is required", ErrInvalidCredentials)
	}
	cookie, err := NormalizeQianwenQuotaCookie(ch.Settings.ProviderQuota.QianwenTokenPlan.AuthCookie)
	if err != nil {
		return QuotaData{}, fmt.Errorf("%w: invalid Qianwen console cookie", ErrInvalidCredentials)
	}
	hc := c.httpClient
	if ch.Settings.Proxy != nil {
		hc = hc.WithProxy(ch.Settings.Proxy)
	}
	hc = hc.WithRejectHTTPSDowngrade()
	request := httpclient.NewRequestBuilder().WithMethod(http.MethodGet).
		WithURL(qianwenUserInfoURL).WithHeader("Cookie", cookie).
		WithHeader("Accept", "application/json").Build()
	body, err := qianwenQuotaRequest(ctx, hc, request)
	if err != nil {
		return QuotaData{}, err
	}
	var user struct {
		Code json.Number `json:"code"`
		Data struct {
			SecToken string `json:"secToken"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &user) != nil || user.Code.String() != "200" || user.Data.SecToken == "" {
		return QuotaData{}, fmt.Errorf("%w: Qianwen console session expired; update its cookie", ErrInvalidCredentials)
	}
	params, err := json.Marshal(map[string]any{
		"Api": qianwenPersonalUsageAPI, "V": "1.0",
		"Data": map[string]any{"cornerstoneParam": map[string]string{
			"domain": "platform.qianwenai.com", "consoleSite": "QIANWENAI", "console": "ONE_CONSOLE",
			"xsp_lang": "zh-CN", "protocol": "V2", "productCode": "p_efm",
		}},
	})
	if err != nil {
		return QuotaData{}, fmt.Errorf("encode Qianwen quota request: %w", err)
	}
	query := url.Values{"product": {"sfm_bailian"}, "action": {"BroadScopeAspnGateway"}, "api": {qianwenPersonalUsageAPI}}
	form := url.Values{
		"product": {"sfm_bailian"}, "action": {"BroadScopeAspnGateway"}, "region": {"cn-beijing"},
		"sec_token": {user.Data.SecToken}, "params": {string(params)},
	}
	request = httpclient.NewRequestBuilder().WithMethod(http.MethodPost).
		WithURL(qianwenGatewayURL+"?"+query.Encode()).WithHeader("Cookie", cookie).
		WithHeader("Origin", "https://platform.qianwenai.com").
		WithHeader("Referer", "https://platform.qianwenai.com/").
		WithHeader("Content-Type", "application/x-www-form-urlencoded").
		WithHeader("Accept", "application/json").WithBody([]byte(form.Encode())).Build()
	body, err = qianwenQuotaRequest(ctx, hc, request)
	if err != nil {
		return QuotaData{}, err
	}
	return parseQianwenTokenPlanQuotaResponse(body)
}

func qianwenQuotaRequest(ctx context.Context, hc *httpclient.HttpClient, request *httpclient.Request) ([]byte, error) {
	resp, err := hc.Do(ctx, request)
	status := 0
	if err != nil {
		if httpErr, ok := errors.AsType[*httpclient.Error](err); ok {
			status = httpErr.StatusCode
		}
	} else {
		status = resp.StatusCode
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return nil, fmt.Errorf("%w: Qianwen console session rejected (%d); update its cookie", ErrInvalidCredentials, status)
	}
	// Provider errors can echo cookies or the security token. Do not persist
	// upstream bodies or transport error strings in the quota dashboard.
	if err != nil || status < 200 || status >= 300 {
		return nil, fmt.Errorf("Qianwen quota request failed (HTTP %d)", status)
	}
	return resp.Body, nil
}

func parseQianwenTokenPlanQuotaResponse(body []byte) (QuotaData, error) {
	responseText := strings.ToLower(string(body))
	for _, code := range []string{"consoleneedlogin", "bailiangateway.login.notlogined", "no_login"} {
		if strings.Contains(responseText, code) {
			return QuotaData{}, fmt.Errorf("%w: Qianwen console session expired; update its cookie", ErrInvalidCredentials)
		}
	}
	var outer struct {
		Code            json.Number     `json:"code"`
		SuccessResponse bool            `json:"successResponse"`
		Data            json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &outer); err != nil {
		return QuotaData{}, fmt.Errorf("invalid Qianwen quota response")
	}
	if outer.Code.String() == "401" || outer.Code.String() == "403" {
		return QuotaData{}, fmt.Errorf("%w: Qianwen console session expired", ErrInvalidCredentials)
	}
	if outer.Code.String() != "200" && !outer.SuccessResponse {
		return QuotaData{}, fmt.Errorf("Qianwen quota gateway rejected the request")
	}
	var gateway struct {
		Success *bool `json:"success"`
		DataV2  struct {
			Data json.RawMessage `json:"data"`
			Ret  []string        `json:"ret"`
		} `json:"DataV2"`
	}
	if json.Unmarshal(outer.Data, &gateway) != nil || (gateway.Success != nil && !*gateway.Success) {
		return QuotaData{}, fmt.Errorf("Qianwen quota gateway returned an error")
	}
	for _, ret := range gateway.DataV2.Ret {
		if !strings.HasPrefix(ret, "SUCCESS") {
			return QuotaData{}, fmt.Errorf("Qianwen quota query failed")
		}
	}
	data := gateway.DataV2.Data
	var wrapped struct {
		Success *bool           `json:"success"`
		Data    json.RawMessage `json:"data"`
	}
	if json.Unmarshal(data, &wrapped) != nil || (wrapped.Success != nil && !*wrapped.Success) {
		return QuotaData{}, fmt.Errorf("Qianwen quota query returned an error")
	}
	if len(wrapped.Data) > 0 {
		data = wrapped.Data
	}
	var usage struct {
		Percentage *float64        `json:"per1WeekPercentage"`
		Reset      json.RawMessage `json:"per1WeekResetTime"`
	}
	if json.Unmarshal(data, &usage) != nil || usage.Percentage == nil ||
		!isFiniteRatio(*usage.Percentage) || *usage.Percentage < 0 {
		return QuotaData{}, fmt.Errorf("Qianwen personal plan returned no valid seven-day usage")
	}
	ratio := min(*usage.Percentage, 1)
	status := "available"
	if ratio >= 1 {
		status = "exhausted"
	} else if ratio >= WarningThresholdRatio {
		status = "warning"
	}
	reset := parseQianwenQuotaReset(usage.Reset)
	return NormalizeQuotaData(QuotaData{
		ProviderType: "qianwen_token_plan", Status: status, Ready: IsReadyStatus(status), NextResetAt: reset,
		RawData: map[string]any{"plan_type": "personal", "used_percent": ratio * 100, "remaining_percent": (1 - ratio) * 100},
		Limits:  []QuotaLimitStatus{NewTokenLimitStatus(status, ratio, reset).WithWindow(QuotaWindow7d, 7*24*time.Hour)},
	}), nil
}

func parseQianwenQuotaReset(raw json.RawMessage) *time.Time {
	value := strings.Trim(string(raw), "\"")
	if millis, err := strconv.ParseInt(value, 10, 64); err == nil && millis > 0 {
		t := time.UnixMilli(millis)
		return &t
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return &t
	}
	// Console timestamps without an offset use the China region's time zone.
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02T15:04:05"} {
		if t, err := time.ParseInLocation(layout, value, time.FixedZone("Asia/Shanghai", 8*60*60)); err == nil {
			return &t
		}
	}
	return nil
}
