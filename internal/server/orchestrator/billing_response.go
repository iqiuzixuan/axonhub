package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

func (s *PersistenceState) publicResponseModel() string {
	if s.Billing == nil {
		return ""
	}
	if s.Billing.Source == objects.BillingModelSourceOriginal {
		return s.Billing.OriginalModel
	}
	if s.RequestExec != nil {
		return s.RequestExec.ModelID
	}
	if s.CurrentCandidate != nil && s.CurrentModelIndex < len(s.CurrentCandidate.Models) {
		return s.CurrentCandidate.Models[s.CurrentModelIndex].ActualModel
	}
	return s.Billing.OriginalModel
}

// Only protocol metadata is rewritten. Generated text and tool arguments are
// never searched/replaced, and binary payloads/DONE markers remain untouched.
func publicResponseJSON(data []byte, model string, hide bool) []byte {
	if model == "" || !gjson.ValidBytes(data) {
		return data
	}
	result := append([]byte(nil), data...)
	for _, path := range []string{"model", "message.model", "response.model", "modelVersion"} {
		if gjson.GetBytes(result, path).Exists() {
			if next, err := sjson.SetBytes(result, path, model); err == nil {
				result = next
			}
		}
	}
	// Upstream cost is not the platform's bill and must never bypass pricing.
	for _, path := range []string{"usage.cost", "message.usage.cost", "response.usage.cost"} {
		if next, err := sjson.DeleteBytes(result, path); err == nil {
			result = next
		}
	}
	if hide {
		for _, path := range []string{"error", "response.error"} {
			if value := gjson.GetBytes(result, path); value.Exists() && value.Type != gjson.Null {
				if next, err := sjson.SetBytes(result, path, map[string]string{"type": "upstream_error", "message": "Upstream request failed"}); err == nil {
					result = next
				}
			}
		}
	}
	return result
}

func (s *PersistenceState) publicRawResponse(response *httpclient.Response) *httpclient.Response {
	if s.Billing == nil || response == nil {
		return response
	}
	copy := *response
	copy.Body = s.publicResponseWithCost(response.Body, &llm.Usage{})
	copy.Headers = s.publicResponseHeaders(response.Headers)
	// Rewritten bodies must not retain the upstream content length or digest.
	copy.Headers.Del("Content-Length")
	copy.Headers.Del("ETag")
	copy.Headers.Del("Content-MD5")
	return &copy
}

func (s *PersistenceState) publicRawStream(stream streams.Stream[*httpclient.StreamEvent]) streams.Stream[*httpclient.StreamEvent] {
	if s.Billing == nil {
		return stream
	}
	usage := &llm.Usage{}
	return streams.Map(stream, func(event *httpclient.StreamEvent) *httpclient.StreamEvent {
		if event == nil || event.IsBinaryAudioChunk() {
			return event
		}
		copy := *event
		copy.Data = s.publicResponseWithCost(event.Data, usage)
		return &copy
	})
}

// Streaming message_start/message_delta split the usage across events. Keep a
// cumulative value so an output-only delta does not erase the input charge.
func (s *PersistenceState) publicResponseWithCost(data []byte, usage *llm.Usage) []byte {
	result := publicResponseJSON(data, s.publicResponseModel(), s.Billing.Source == objects.BillingModelSourceOriginal)
	if !s.Billing.InjectCost {
		return result
	}
	var snapshot *objects.RequestBilling
	if s.Billing.Source == objects.BillingModelSourceOriginal {
		snapshot = s.Billing
	} else if s.RequestExec != nil {
		snapshot = s.RequestExec.CostPrice
	}
	if snapshot == nil || snapshot.Price == nil {
		return result
	}
	for _, path := range []string{"usage", "message.usage", "response.usage"} {
		raw := gjson.GetBytes(data, path)
		if !raw.IsObject() {
			continue
		}
		if raw.Get("prompt_tokens").Exists() {
			_ = json.Unmarshal([]byte(raw.Raw), usage)
		} else {
			if input := raw.Get("input_tokens"); input.Exists() {
				usage.PromptTokens = input.Int()
				usage.PromptTokensDetails = &llm.PromptTokensDetails{CachedTokens: raw.Get("input_tokens_details.cached_tokens").Int()}
				if cache := raw.Get("cache_read_input_tokens"); cache.Exists() {
					usage.PromptTokensDetails.CachedTokens = cache.Int()
					usage.PromptTokens += cache.Int()
				}
				if cache := raw.Get("cache_creation_input_tokens"); cache.Exists() {
					usage.PromptTokensDetails.WriteCachedTokens = cache.Int()
					usage.PromptTokens += cache.Int()
				}
				usage.PromptTokensDetails.WriteCached5MinTokens = raw.Get("cache_creation.ephemeral_5m_input_tokens").Int()
				usage.PromptTokensDetails.WriteCached1HourTokens = raw.Get("cache_creation.ephemeral_1h_input_tokens").Int()
			}
			if output := raw.Get("output_tokens"); output.Exists() {
				usage.CompletionTokens = output.Int()
			}
		}
		usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
		_, cost := biz.ComputeUsageCost(usage, *snapshot.Price, snapshot.At)
		if patched, err := sjson.SetBytes(result, path+".cost", cost.InexactFloat64()); err == nil {
			result = patched
		}
	}
	return result
}

func (s *PersistenceState) publicResponseHeaders(headers http.Header) http.Header {
	result := headers.Clone()
	if s.Billing == nil || s.Billing.Source != objects.BillingModelSourceOriginal {
		return result
	}
	internalModels := []string{s.OriginalModel}
	if s.RequestExec != nil {
		internalModels = append(internalModels, s.RequestExec.ModelID)
	}
	for key, values := range result {
		if strings.Contains(strings.ToLower(key), "model") {
			result.Del(key)
			continue
		}
		for _, model := range internalModels {
			if model == "" || model == s.Billing.OriginalModel {
				continue
			}
			for _, value := range values {
				if strings.Contains(value, model) {
					result.Del(key)
				}
			}
		}
	}
	return result
}
