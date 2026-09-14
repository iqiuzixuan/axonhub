package orchestrator

import (
	"context"

	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/pipeline"
)

type requestBillingMiddleware struct {
	pipeline.DummyMiddleware
	inbound *PersistentInboundTransformer
}

func prepareRequestBilling(inbound *PersistentInboundTransformer) pipeline.Middleware {
	return &requestBillingMiddleware{inbound: inbound}
}
func (m *requestBillingMiddleware) Name() string { return "prepare-request-billing" }
func (m *requestBillingMiddleware) OnInboundLlmRequest(ctx context.Context, req *llm.Request) (*llm.Request, error) {
	if m.inbound.state.Billing == nil {
		original := m.inbound.state.ClientModel
		if original == "" {
			original = req.Model
		}
		snapshot, err := m.inbound.state.UsageLogService.PrepareBilling(ctx, original)
		if err != nil {
			return nil, err
		}
		m.inbound.state.Billing = snapshot
	}
	return req, nil
}
