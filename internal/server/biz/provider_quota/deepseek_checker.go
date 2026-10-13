package provider_quota

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/llm/httpclient"
)

const deepSeekBalanceURL = "https://api.deepseek.com/user/balance"

var deepSeekAmountPattern = regexp.MustCompile(`^\d+(\.\d+)?$`)

// DeepSeekBalance stores only the available total and its visual reference.
// BaselineBalance is an observed starting balance, not an upstream spending limit.
type DeepSeekBalance struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	BaselineBalance string `json:"baseline_balance"`
}

type DeepSeekQuotaChecker struct {
	httpClient *httpclient.HttpClient
}

func NewDeepSeekQuotaChecker(client *httpclient.HttpClient) *DeepSeekQuotaChecker {
	return &DeepSeekQuotaChecker{httpClient: client}
}

func (c *DeepSeekQuotaChecker) SupportsChannel(ch *ent.Channel) bool {
	if ch.Type != channel.TypeDeepseek && ch.Type != channel.TypeDeepseekAnthropic {
		return false
	}
	if strings.TrimSpace(ch.BaseURL) == "" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(ch.BaseURL))
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api.deepseek.com") &&
		(u.Port() == "" || u.Port() == "443") && u.User == nil
}

func (c *DeepSeekQuotaChecker) CheckQuota(ctx context.Context, ch *ent.Channel) (QuotaData, error) {
	// A third-party DeepSeek-compatible endpoint has its own billing account.
	// Never send its credentials to the official balance API.
	if !c.SupportsChannel(ch) {
		return QuotaData{}, fmt.Errorf("DeepSeek balance collection requires an official API channel")
	}
	apiKey := strings.TrimSpace(ch.Credentials.APIKey)
	if apiKey == "" {
		for _, key := range ch.Credentials.APIKeys {
			if apiKey = strings.TrimSpace(key); apiKey != "" {
				break
			}
		}
	}
	if apiKey == "" {
		return QuotaData{}, fmt.Errorf("channel has no API key")
	}

	request := httpclient.NewRequestBuilder().
		WithMethod(http.MethodGet).
		WithURL(deepSeekBalanceURL).
		WithBearerToken(apiKey).
		WithHeader("Accept", "application/json").
		Build()
	client := c.httpClient
	if ch.Settings != nil && ch.Settings.Proxy != nil {
		client = client.WithProxy(ch.Settings.Proxy)
	}
	response, err := client.Do(ctx, request)
	if err != nil {
		var httpErr *httpclient.Error
		if errors.As(err, &httpErr) {
			if httpErr.StatusCode == http.StatusUnauthorized || httpErr.StatusCode == http.StatusForbidden {
				return QuotaData{}, fmt.Errorf("DeepSeek balance HTTP %d: %w", httpErr.StatusCode, ErrInvalidCredentials)
			}
			return QuotaData{}, fmt.Errorf("DeepSeek balance HTTP %d", httpErr.StatusCode)
		}
		return QuotaData{}, fmt.Errorf("DeepSeek balance request failed: %w", err)
	}
	if response.StatusCode != http.StatusOK {
		// Do not include the response body: errors may echo authentication data.
		return QuotaData{}, fmt.Errorf("DeepSeek balance HTTP %d", response.StatusCode)
	}

	digest := sha256.Sum256([]byte("deepseek:" + apiKey))
	identity := hex.EncodeToString(digest[:])
	var previous map[string]any
	if status := ch.Edges.ProviderQuotaStatus; status != nil && status.ProviderType == "deepseek" {
		previous = status.QuotaData
	}
	return parseDeepSeekBalance(response.Body, identity, previous)
}

func parseDeepSeekAmount(value string) (decimal.Decimal, error) {
	if len(value) > 64 || !deepSeekAmountPattern.MatchString(value) {
		return decimal.Zero, fmt.Errorf("invalid DeepSeek balance amount")
	}
	return decimal.NewFromString(value)
}

func parseDeepSeekBalance(body []byte, identity string, previous map[string]any) (QuotaData, error) {
	var response struct {
		Available *bool             `json:"is_available"`
		Balances  []DeepSeekBalance `json:"balance_infos"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return QuotaData{}, fmt.Errorf("failed to parse DeepSeek balance response")
	}
	if response.Available == nil || len(response.Balances) == 0 {
		return QuotaData{}, fmt.Errorf("DeepSeek balance response missing availability or balances")
	}

	oldBalances := make(map[string]DeepSeekBalance)
	if previous["_balance_identity"] == identity {
		// Round-trip handles both in-process structs and JSON reloaded from SQL.
		encoded, err := json.Marshal(previous["balance_infos"])
		var balances []DeepSeekBalance
		if err == nil && json.Unmarshal(encoded, &balances) == nil {
			for _, balance := range balances {
				oldBalances[balance.Currency] = balance
			}
		}
	}

	seen := make(map[string]bool)
	hasBalance := false
	for i := range response.Balances {
		balance := &response.Balances[i]
		if (balance.Currency != "CNY" && balance.Currency != "USD") || seen[balance.Currency] {
			return QuotaData{}, fmt.Errorf("DeepSeek balance response has invalid or duplicate currency")
		}
		seen[balance.Currency] = true
		amount, err := parseDeepSeekAmount(balance.TotalBalance)
		if err != nil {
			return QuotaData{}, err
		}
		baseline := amount
		if old, ok := oldBalances[balance.Currency]; ok && amount.IsPositive() {
			last, lastErr := parseDeepSeekAmount(old.TotalBalance)
			base, baseErr := parseDeepSeekAmount(old.BaselineBalance)
			if lastErr == nil && baseErr == nil && base.IsPositive() &&
				last.LessThanOrEqual(base) && amount.LessThanOrEqual(last) {
				baseline = base
			}
		}
		balance.TotalBalance = amount.String()
		balance.BaselineBalance = baseline.String()
		hasBalance = hasBalance || amount.IsPositive()
	}

	status := "exhausted"
	if *response.Available && hasBalance {
		status = "available"
	}
	// The visual baseline must not become a synthetic token limit: it is not
	// the provider's quota and must not trigger BACKPRESSURE routing.
	return QuotaData{
		ProviderType: "deepseek",
		Status:       status,
		Ready:        IsReadyStatus(status),
		RawData: map[string]any{
			"balance_infos":     response.Balances,
			"_balance_identity": identity,
		},
	}, nil
}
