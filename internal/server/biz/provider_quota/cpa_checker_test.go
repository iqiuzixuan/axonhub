package provider_quota

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/providerquotastatus"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/stretchr/testify/require"
)

func cpaTestChannel(base string) *ent.Channel {
	return &ent.Channel{Type: channel.TypeCodex, BaseURL: "https://inference.invalid/backend-api", Credentials: objects.ChannelCredentials{APIKey: "inference-only", ManagementAPIKey: "management-only"}, Settings: &objects.ChannelSettings{ProviderQuota: &objects.ChannelProviderQuotaSettings{CPA: &objects.CPAQuotaSettings{ManagementURL: base}}}}
}

func cpaTestUsage(five, weekly int) string {
	return fmt.Sprintf(`{"plan_type":"pro","rate_limit":{"allowed":true,"primary_window":{"used_percent":%d,"limit_window_seconds":18000,"reset_after_seconds":3600},"secondary_window":{"used_percent":%d,"limit_window_seconds":604800,"reset_after_seconds":86400}}}`, five, weekly)
}

func TestCPAQuotaChecker_ProtocolsAndCredentialSeparation(t *testing.T) {
	for _, version := range []objects.CPAManagementAPIVersion{objects.CPAManagementAPIVersionV8, objects.CPAManagementAPIVersionV0} {
		t.Run(string(version), func(t *testing.T) {
			var calls atomic.Int32
			root, listPath, callPath := "/prefix/v8/management", "/credentials", "/requests/api-call"
			if version == objects.CPAManagementAPIVersionV0 {
				root, listPath, callPath = "/prefix/v0/management", "/auth-files", "/api-call"
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "Bearer management-only", r.Header.Get("Authorization"))
				if r.URL.Path == root+listPath {
					require.Equal(t, http.MethodGet, r.Method)
					fmt.Fprint(w, `{"files":[{"provider":"codex","auth_index":"account-one","id_token":{"chatgpt_account_id":"chatgpt-account"}},{"provider":"codex","auth_index":"disabled","disabled":true},{"provider":"claude","auth_index":"other"}]}`)
					return
				}
				require.Equal(t, root+callPath, r.URL.Path)
				var body struct {
					AuthIndex string            `json:"auth_index"`
					Method    string            `json:"method"`
					URL       string            `json:"url"`
					Header    map[string]string `json:"header"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				require.Equal(t, "account-one", body.AuthIndex)
				require.Equal(t, http.MethodGet, body.Method)
				require.Equal(t, "https://chatgpt.com/backend-api/wham/usage", body.URL)
				require.Equal(t, "Bearer $TOKEN$", body.Header["Authorization"])
				require.Equal(t, "chatgpt-account", body.Header["Chatgpt-Account-Id"])
				_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": `{"plan_type":"pro","rate_limit":{"allowed":true,"primary_window":{"used_percent":11,"limit_window_seconds":604800,"reset_after_seconds":3600}}}`})
			}))
			defer server.Close()
			ch := cpaTestChannel(server.URL + "/prefix/")
			ch.Settings.ProviderQuota.CPA.APIVersion = version
			quota, err := NewCPAQuotaChecker(httpclient.NewHttpClientWithClient(server.Client())).CheckQuota(t.Context(), ch)
			require.NoError(t, err)
			require.Equal(t, int32(2), calls.Load())
			require.Equal(t, "available", quota.Status)
			require.Len(t, quota.Limits, 1)
			require.Equal(t, QuotaWindow7d, quota.Limits[0].Window)
			require.InDelta(t, .11, quota.Limits[0].UsageRatio, .0001)
			require.Nil(t, quota.Limits[0].PeriodStart)
			encoded, err := json.Marshal(quota.RawData)
			require.NoError(t, err)
			for _, secret := range []string{"inference-only", "management-only", "chatgpt-account", "$TOKEN$"} {
				require.NotContains(t, string(encoded), secret)
			}
		})
	}
}

func TestCPAQuotaChecker_PoolRequiresAllWindowsOnOneAccount(t *testing.T) {
	for _, test := range []struct {
		name      string
		bodies    map[string]string
		want      string
		wantError bool
		partial   bool
	}{
		{"cross-exhausted", map[string]string{"a": cpaTestUsage(100, 10), "b": cpaTestUsage(10, 100)}, "exhausted", false, false},
		{"healthy-alternative", map[string]string{"a": cpaTestUsage(100, 10), "b": cpaTestUsage(20, 30)}, "available", false, false},
		{"partial-with-capacity", map[string]string{"a": "", "b": cpaTestUsage(20, 30)}, "available", false, true},
		{"partial-without-capacity", map[string]string{"a": "", "b": cpaTestUsage(100, 30)}, "", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/credentials") {
					fmt.Fprint(w, `{"files":[{"provider":"codex","auth_index":"a","label":"same"},{"provider":"codex","auth_index":"b","label":"same"}]}`)
					return
				}
				var body struct {
					AuthIndex string `json:"auth_index"`
				}
				require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
				status := 200
				if test.bodies[body.AuthIndex] == "" {
					status = 401
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"status_code": status, "body": test.bodies[body.AuthIndex]})
			}))
			defer server.Close()
			quota, err := NewCPAQuotaChecker(httpclient.NewHttpClientWithClient(server.Client())).CheckQuota(t.Context(), cpaTestChannel(server.URL))
			if test.wantError {
				require.ErrorIs(t, err, ErrInvalidCredentials)
				require.Empty(t, quota.Limits)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.want, quota.Status)
			require.Equal(t, test.partial, quota.RawData["partial"])
			status, ready := EffectiveStatus(quota.Limits, providerquotastatus.Status(quota.Status), quota.Ready, QuotaLimitTypeToken)
			require.Equal(t, providerquotastatus.Status(test.want), status)
			require.Equal(t, test.want != "exhausted", ready)
			state, _ := EvaluateQuotaRouting(quota.Limits, quota.Status, QuotaLimitTypeToken, time.Now())
			if test.want == "exhausted" {
				require.Equal(t, RoutingExhausted, state)
			} else {
				require.Equal(t, RoutingOpen, state)
			}
			if !test.partial {
				require.NotEqual(t, quota.Limits[0].Account, quota.Limits[2].Account)
			}
		})
	}
}

func TestCPAQuotaChecker_UnknownDataAndAuthenticationNeverFabricateQuota(t *testing.T) {
	for _, body := range []string{`{}`, `{"rate_limit":{"primary_window":{"limit_window_seconds":604800}}}`, `{"rate_limit":{"primary_window":{"used_percent":-1,"limit_window_seconds":604800}}}`, `{"rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":0}}}`} {
		_, err := parseCPAUsage([]byte(body))
		require.Error(t, err)
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(401)
		fmt.Fprint(w, "management-only secret-body")
	}))
	defer server.Close()
	_, err := NewCPAQuotaChecker(httpclient.NewHttpClientWithClient(server.Client())).CheckQuota(t.Context(), cpaTestChannel(server.URL))
	require.ErrorIs(t, err, ErrInvalidCredentials)
	require.NotContains(t, err.Error(), "management-only")
	require.NotContains(t, err.Error(), "secret-body")
	require.Equal(t, int32(1), calls.Load())
}

func TestCPAQuotaChecker_ExplicitSelectionAndOAuthGuard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/credentials") {
			fmt.Fprint(w, `{"files":[{"provider":"codex","auth_index":"a"},{"provider":"codex","auth_index":"b"}]}`)
			return
		}
		var body struct {
			AuthIndex string `json:"auth_index"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Equal(t, "b", body.AuthIndex)
		_ = json.NewEncoder(w).Encode(map[string]any{"status_code": 200, "body": cpaTestUsage(10, 20)})
	}))
	defer server.Close()
	checker := NewCPAQuotaChecker(httpclient.NewHttpClientWithClient(server.Client()))
	ch := cpaTestChannel(server.URL)
	ch.Settings.ProviderQuota.CPA.AuthIndex = " b "
	quota, err := checker.CheckQuota(context.Background(), ch)
	require.NoError(t, err)
	require.Len(t, quota.Limits, 2)
	ch.Settings.ProviderQuota.CPA.AuthIndex = "removed"
	_, err = checker.CheckQuota(t.Context(), ch)
	require.ErrorIs(t, err, ErrInvalidCredentials)
	ch.Credentials.OAuth = &objects.OAuthCredentials{AccessToken: "oauth-secret"}
	_, err = checker.CheckQuota(t.Context(), ch)
	require.ErrorIs(t, err, ErrInvalidCredentials)
}

func TestNormalizeCPASettings(t *testing.T) {
	for _, suffix := range []string{"/", "/v8/management/", "/v0/management"} {
		settings, err := NormalizeCPASettings(objects.CPAQuotaSettings{ManagementURL: "https://example.com/prefix" + suffix, AuthIndex: " abc "})
		require.NoError(t, err)
		require.Equal(t, "https://example.com/prefix", settings.ManagementURL)
		require.Equal(t, objects.CPAManagementAPIVersionV8, settings.APIVersion)
		require.Equal(t, "abc", settings.AuthIndex)
	}
	for _, address := range []string{"", "file:///tmp/cpa", "https://user:password@example.com", "http://example.com?key=secret", "http://example.com#token"} {
		_, err := NormalizeCPASettings(objects.CPAQuotaSettings{ManagementURL: address})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "password")
	}
}
