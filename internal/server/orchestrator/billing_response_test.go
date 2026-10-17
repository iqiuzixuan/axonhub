package orchestrator

import (
	"context"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
	"github.com/looplj/axonhub/llm/streams"
	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"testing"
	"time"
)

func TestPublicResponseModelMetadata(t *testing.T) {
	input := []byte(`{"model":"secret-C","choices":[{"message":{"content":"secret-C is user text"}}],"response":{"model":"secret-C","usage":{"cost":999}},"error":{"message":"secret-C unavailable"}}`)
	got := publicResponseJSON(input, "public-A", true)
	require.Equal(t, "public-A", gjson.GetBytes(got, "model").String())
	require.Equal(t, "public-A", gjson.GetBytes(got, "response.model").String())
	require.Equal(t, "secret-C is user text", gjson.GetBytes(got, "choices.0.message.content").String())
	require.False(t, gjson.GetBytes(got, "response.usage.cost").Exists())
	require.NotContains(t, gjson.GetBytes(got, "error").Raw, "secret-C")
	require.Equal(t, "secret-C", gjson.GetBytes(input, "model").String(), "provider evidence must not be modified")
	require.Equal(t, []byte("[DONE]"), publicResponseJSON([]byte("[DONE]"), "A", true))
}

func TestPublicResponseStreamCostAccumulates(t *testing.T) {
	for _, source := range []objects.BillingModelSource{objects.BillingModelSourceOriginal, objects.BillingModelSourceRedirected} {
		t.Run(string(source), func(t *testing.T) {
			s := &PersistenceState{Billing: &objects.RequestBilling{Source: source, OriginalModel: "A", InjectCost: true, At: time.Now(), Price: &objects.ModelPrice{Items: []objects.ModelPriceItem{
				{ItemCode: objects.PriceItemCodeUsage, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(10))}},
				{ItemCode: objects.PriceItemCodeCompletion, Pricing: objects.Pricing{Mode: objects.PricingModeUsagePerUnit, UsagePerUnit: lo.ToPtr(decimal.NewFromInt(20))}},
			}}}}
			s.RequestExec = &ent.RequestExecution{ModelID: "C"}
			wantModel := "A"
			if source == objects.BillingModelSourceRedirected {
				wantModel = "C"
			}
			usage := &llm.Usage{}
			start := s.publicResponseWithCost([]byte(`{"type":"message_start","message":{"model":"C","usage":{"input_tokens":1000000,"output_tokens":0}}}`), usage)
			require.Equal(t, wantModel, gjson.GetBytes(start, "message.model").String())
			require.Equal(t, float64(10), gjson.GetBytes(start, "message.usage.cost").Float())
			end := s.publicResponseWithCost([]byte(`{"type":"message_delta","usage":{"output_tokens":100000}}`), usage)
			require.Equal(t, float64(12), gjson.GetBytes(end, "usage.cost").Float())
		})
	}
}

func TestUnpricedOriginalResponseKeepsPublicModelWithoutCost(t *testing.T) {
	s := &PersistenceState{Billing: &objects.RequestBilling{
		Source: objects.BillingModelSourceOriginal, OriginalModel: "codex-auto-review", InjectCost: true,
	}, RequestExec: &ent.RequestExecution{CostPrice: &objects.RequestBilling{Price: &objects.ModelPrice{}}}}
	for _, payload := range []struct{ body, modelPath, costPath string }{
		{`{"model":"internal-actual","usage":{"prompt_tokens":10,"cost":9}}`, "model", "usage.cost"},
		{`{"type":"response.completed","response":{"model":"internal-actual","usage":{"input_tokens":10,"cost":9}}}`, "response.model", "response.usage.cost"},
		{`{"type":"message_start","message":{"model":"internal-actual","usage":{"input_tokens":10,"cost":9}}}`, "message.model", "message.usage.cost"},
	} {
		got := s.publicResponseWithCost([]byte(payload.body), &llm.Usage{})
		require.Equal(t, "codex-auto-review", gjson.GetBytes(got, payload.modelPath).String())
		require.False(t, gjson.GetBytes(got, payload.costPath).Exists())
	}
}

type billingDeliveryProbe struct {
	streams.Stream[*httpclient.StreamEvent]
	acknowledged bool
	finalized    bool
	delivered    bool
}

func (s *billingDeliveryProbe) AcknowledgeDelivery()       { s.acknowledged = true }
func (s *billingDeliveryProbe) DeliveryAcknowledged() bool { return s.acknowledged }
func (s *billingDeliveryProbe) WaitForFinalization()       { s.finalized = true }
func (s *billingDeliveryProbe) Deliver(ctx context.Context, write func() (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.delivered = true
	terminal, err := write()
	if terminal && err == nil {
		s.AcknowledgeDelivery()
	}
	return err
}
func TestPublicRawStreamPreservesDeliveryCapabilities(t *testing.T) {
	raw := &httpclient.StreamEvent{Data: []byte(`{"model":"internal-model","usage":{"cost":99}}`)}
	source := &billingDeliveryProbe{Stream: streams.SliceStream([]*httpclient.StreamEvent{raw})}
	state := &PersistenceState{Billing: &objects.RequestBilling{Source: objects.BillingModelSourceOriginal, OriginalModel: "public-model", InjectCost: true}}
	projected := state.publicRawStream(source)
	require.True(t, projected.Next())
	event := projected.Current()
	require.Equal(t, "public-model", gjson.GetBytes(event.Data, "model").String())
	require.False(t, gjson.GetBytes(event.Data, "usage.cost").Exists())
	require.False(t, source.acknowledged, "reading the terminal must not acknowledge delivery")
	boundary, ok := projected.(streams.DeliveryBoundary)
	require.True(t, ok)
	require.NoError(t, boundary.Deliver(t.Context(), func() (bool, error) { return true, nil }))
	require.True(t, source.delivered)
	require.True(t, projected.(streams.DeliveryAcknowledger).DeliveryAcknowledged())
	projected.(streams.FinalizationWaiter).WaitForFinalization()
	require.True(t, source.finalized)
	require.Equal(t, "internal-model", gjson.GetBytes(raw.Data, "model").String())
}
