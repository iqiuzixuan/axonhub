package provider_quota

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
)

const CPAAccountsAvailabilityGroup = "cpa_accounts"

type CPAQuotaChecker struct{ httpClient *httpclient.HttpClient }

func NewCPAQuotaChecker(client *httpclient.HttpClient) *CPAQuotaChecker {
	return &CPAQuotaChecker{httpClient: client}
}

func CPASettings(ch *ent.Channel) *objects.CPAQuotaSettings {
	if ch.Type != channel.TypeCodex || ch.Settings == nil || ch.Settings.ProviderQuota == nil {
		return nil
	}
	return ch.Settings.ProviderQuota.CPA
}

// NormalizeCPASettings preserves reverse-proxy prefixes and accepts either the
// service URL or a management API root. It never guesses from inference URLs.
func NormalizeCPASettings(settings objects.CPAQuotaSettings) (objects.CPAQuotaSettings, error) {
	u, err := url.Parse(strings.TrimSpace(settings.ManagementURL))
	if err != nil || u == nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return settings, fmt.Errorf("CPA management URL must be an HTTP(S) service URL without credentials, query or fragment")
	}
	u.Path = strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/v8/management", "/v0/management"} {
		u.Path = strings.TrimSuffix(u.Path, suffix)
	}
	u.RawPath = ""
	settings.ManagementURL = strings.TrimRight(u.String(), "/")
	settings.AuthIndex = strings.TrimSpace(settings.AuthIndex)
	if settings.APIVersion == "" {
		settings.APIVersion = objects.CPAManagementAPIVersionV8
	}
	if settings.APIVersion != objects.CPAManagementAPIVersionV8 && settings.APIVersion != objects.CPAManagementAPIVersionV0 {
		return settings, fmt.Errorf("unsupported CPA management API version")
	}
	return settings, nil
}

type cpaCredential struct {
	AuthIndex string `json:"auth_index"`
	Provider  string `json:"provider"`
	Type      string `json:"type"`
	Label     string `json:"label"`
	Disabled  bool   `json:"disabled"`
	IDToken   struct {
		AccountID string `json:"chatgpt_account_id"`
	} `json:"id_token"`
}

type cpaAccountQuota struct {
	Ref      string             `json:"ref"`
	Label    string             `json:"label"`
	Status   string             `json:"status"`
	PlanType string             `json:"plan_type,omitempty"`
	Limits   []QuotaLimitStatus `json:"limits,omitempty"`
}

func (c *CPAQuotaChecker) SupportsChannel(ch *ent.Channel) bool { return CPASettings(ch) != nil }

func (c *CPAQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	configured := CPASettings(ch)
	if configured == nil || ch.Credentials.IsOAuth() || strings.TrimSpace(ch.Credentials.ManagementAPIKey) == "" {
		return QuotaData{}, fmt.Errorf("%w: CPA requires a third-party Codex channel and management key", ErrInvalidCredentials)
	}
	settings, err := NormalizeCPASettings(*configured)
	if err != nil {
		return QuotaData{}, fmt.Errorf("%w: %s", ErrInvalidCredentials, err)
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	hc := c.httpClient.WithRejectHTTPSDowngrade()
	if ch.Settings.Proxy != nil {
		hc = hc.WithProxy(ch.Settings.Proxy).WithRejectHTTPSDowngrade()
	}
	root, credentialsPath, callPath := settings.ManagementURL+"/v8/management", "/credentials", "/requests/api-call"
	if settings.APIVersion == objects.CPAManagementAPIVersionV0 {
		root, credentialsPath, callPath = settings.ManagementURL+"/v0/management", "/auth-files", "/api-call"
	}
	key := strings.TrimSpace(ch.Credentials.ManagementAPIKey)
	var listing struct {
		Files []cpaCredential `json:"files"`
	}
	if err := cpaManagementRequest(ctx, hc, key, root+credentialsPath, nil, &listing); err != nil {
		return QuotaData{}, err
	}
	accounts := make([]cpaCredential, 0, len(listing.Files))
	seen := make(map[string]bool)
	for _, account := range listing.Files {
		provider := account.Provider
		if provider == "" {
			provider = account.Type
		}
		if provider != "codex" || account.Disabled || account.AuthIndex == "" || seen[account.AuthIndex] {
			continue
		}
		if settings.AuthIndex != "" && settings.AuthIndex != account.AuthIndex {
			continue
		}
		seen[account.AuthIndex] = true
		accounts = append(accounts, account)
	}
	if len(accounts) == 0 {
		return QuotaData{}, fmt.Errorf("%w: no matching enabled CPA Codex account", ErrInvalidCredentials)
	}

	results := make([]cpaAccountQuota, len(accounts))
	group := new(errgroup.Group)
	group.SetLimit(4)
	for i, account := range accounts {
		group.Go(func() error {
			label := strings.TrimSpace(account.Label)
			if label == "" {
				label = account.AuthIndex
			} else {
				label = fmt.Sprintf("%s · %s", label, account.AuthIndex)
			}
			// Keep labels unique without exporting emails, token files or OAuth tokens.
			results[i] = cpaAccountQuota{Ref: account.AuthIndex, Label: label, Status: "unknown"}
			headers := map[string]string{"Authorization": "Bearer $TOKEN$", "Content-Type": "application/json"}
			if account.IDToken.AccountID != "" {
				headers["Chatgpt-Account-Id"] = account.IDToken.AccountID
			}
			body := map[string]any{"auth_index": account.AuthIndex, "method": http.MethodGet, "url": "https://chatgpt.com/backend-api/wham/usage", "header": headers}
			var response struct {
				StatusCode int    `json:"status_code"`
				Body       string `json:"body"`
			}
			if err := cpaManagementRequest(ctx, hc, key, root+callPath, body, &response); err != nil || response.StatusCode != http.StatusOK {
				return nil
			}
			quota, err := parseCPAUsage([]byte(response.Body))
			if err != nil {
				return nil
			}
			results[i].Status = quota.Status
			results[i].PlanType, _ = quota.RawData["plan_type"].(string)
			for _, limit := range quota.Limits {
				limit.Account = label
				if len(accounts) == 1 {
					limit.Account = ""
				}
				limit.AvailabilityGroup = CPAAccountsAvailabilityGroup
				// Every required window must be available on the same account.
				// Propagate account availability so OR routing cannot combine
				// spare 5h capacity of one exhausted account with 7d of another.
				limit.Status, limit.Ready = quota.Status, quota.Ready
				// AxonHub sees only part of the traffic behind this CPA account.
				// Its local spend cannot estimate this account's monetary quota.
				limit.PeriodStart = nil
				results[i].Limits = append(results[i].Limits, limit)
			}
			return nil
		})
	}
	_ = group.Wait()
	status, unknown := "exhausted", false
	var limits []QuotaLimitStatus
	for _, result := range results {
		limits = append(limits, result.Limits...)
		if result.Status == "unknown" {
			unknown = true
		}
		if IsReadyStatus(result.Status) && (status == "exhausted" || result.Status == "available") {
			status = result.Status
		}
	}
	if status == "exhausted" && unknown {
		// Failed reads must not turn into a fabricated zero or stale pool state.
		return QuotaData{}, fmt.Errorf("%w: CPA account pool quota is incomplete", ErrInvalidCredentials)
	}
	return NormalizeQuotaData(QuotaData{ProviderType: "codex", Status: status, Ready: IsReadyStatus(status), Limits: limits,
		RawData: map[string]any{"quota_source": "cpa", "accounts": results, "partial": unknown}}), nil
}

func parseCPAUsage(body []byte) (QuotaData, error) {
	var usage CodexUsageResponse
	if err := json.Unmarshal(body, &usage); err != nil || usage.RateLimit == nil {
		return QuotaData{}, fmt.Errorf("invalid CPA Codex usage")
	}
	count := 0
	for _, window := range []*CodeUsageWindow{usage.RateLimit.PrimaryWindow, usage.RateLimit.SecondaryWindow} {
		if window == nil {
			continue
		}
		if window.UsedPercent == nil || math.IsNaN(*window.UsedPercent) || math.IsInf(*window.UsedPercent, 0) || *window.UsedPercent < 0 || window.LimitWindowSeconds == nil || *window.LimitWindowSeconds <= 0 {
			return QuotaData{}, fmt.Errorf("invalid CPA Codex window")
		}
		count++
	}
	if count == 0 {
		return QuotaData{}, fmt.Errorf("CPA Codex usage has no windows")
	}
	return (&CodexQuotaChecker{}).parseResponse(body)
}

func cpaManagementRequest(ctx context.Context, hc *httpclient.HttpClient, key, endpoint string, body any, result any) error {
	builder := httpclient.NewRequestBuilder().WithURL(endpoint).WithMethod(http.MethodGet).WithBearerToken(key)
	if body != nil {
		builder.WithMethod(http.MethodPost).WithHeader("Content-Type", "application/json").WithBody(body)
	}
	response, err := hc.Do(ctx, builder.Build())
	if err != nil {
		var httpErr *httpclient.Error
		if errors.As(err, &httpErr) {
			return fmt.Errorf("%w: CPA management HTTP %d", ErrInvalidCredentials, httpErr.StatusCode)
		}
		// Do not retain raw management response bodies or transport URLs in errors.
		return fmt.Errorf("%w: CPA management request failed", ErrInvalidCredentials)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%w: CPA management HTTP %d", ErrInvalidCredentials, response.StatusCode)
	}
	if err := json.Unmarshal(response.Body, result); err != nil {
		return fmt.Errorf("%w: invalid CPA management response", ErrInvalidCredentials)
	}
	return nil
}
