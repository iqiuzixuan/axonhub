package biz

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xerrors"
	"github.com/looplj/axonhub/llm"
	"github.com/samber/lo"
)

func (s *SystemService) BillingModelSource(ctx context.Context) (objects.BillingModelSource, error) {
	settings, err := s.ModelSettings(ctx)
	if err != nil {
		return "", err
	}
	if !settings.BillingModelSource.Valid() {
		return "", fmt.Errorf("invalid billing model source")
	}
	return settings.BillingModelSource, nil
}

// PrepareBilling fails closed on missing prices; it never falls back to a
// different model's price. An explicit empty price is a free public model.
func (s *UsageLogService) PrepareBilling(ctx context.Context, original string) (*objects.RequestBilling, error) {
	source, err := s.SystemService.BillingModelSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("load billing policy: %w", err)
	}
	snapshot := &objects.RequestBilling{Source: source, OriginalModel: original, At: time.Now()}
	snapshot.InjectCost, _ = s.SystemService.InjectUsageCostEnabled(ctx)
	if source != objects.BillingModelSourceOriginal {
		return snapshot, nil
	}
	entity, err := authz.RunWithSystemBypass(ctx, "request-model-billing-price", func(ctx context.Context) (*ent.Model, error) {
		return s.entFromContext(ctx).Model.Query().Where(model.ModelIDEQ(original), model.StatusEQ(model.StatusEnabled)).Only(ctx)
	})
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("load request model price: %w", err)
	}
	if entity == nil || entity.Settings == nil || entity.Settings.BillingPrice == nil {
		return nil, xerrors.ValidationError("request model has no billing price configured")
	}
	if err := entity.Settings.BillingPrice.Validate(); err != nil {
		return nil, xerrors.ValidationError("request model billing price is invalid")
	}
	raw, err := json.Marshal(entity.Settings.BillingPrice)
	if err != nil {
		return nil, err
	}
	// Copy out of the model object so later configuration edits cannot mutate it.
	if err := json.Unmarshal(raw, &snapshot.Price); err != nil {
		return nil, err
	}
	snapshot.PriceReference = fmt.Sprintf("model:%d:%x", entity.ID, sha256.Sum256(raw))
	return snapshot, nil
}

func requestBillingCost(snapshot *objects.RequestBilling, usage *llm.Usage) ([]objects.CostItem, *float64, string) {
	if snapshot == nil || snapshot.Price == nil || usage == nil {
		return nil, nil, ""
	}
	items, total := ComputeUsageCost(usage, *snapshot.Price, snapshot.At)
	return items, lo.ToPtr(total.InexactFloat64()), snapshot.PriceReference
}

func (s *UsageLogService) InjectRequestUsageCost(ctx context.Context, req *ent.Request, execution *ent.RequestExecution, usage *llm.Usage) {
	if usage == nil || req == nil || execution == nil {
		return
	}
	if req.Billing != nil && req.Billing.Source == objects.BillingModelSourceOriginal {
		_, usage.Cost, _ = requestBillingCost(req.Billing, usage)
		return
	}
	if execution.CostPrice != nil {
		_, usage.Cost, _ = requestBillingCost(execution.CostPrice, usage)
		return
	}
	s.InjectUsageCost(ctx, execution.ChannelID, execution.ModelID, usage)
}

// SnapshotChannelPrice freezes one outbound attempt's cost and redirected price.
func (s *UsageLogService) SnapshotChannelPrice(channelID int, actual string) (*objects.RequestBilling, error) {
	snapshot := &objects.RequestBilling{Source: objects.BillingModelSourceRedirected, OriginalModel: actual, At: time.Now()}
	ch := s.ChannelService.GetEnabledChannel(channelID)
	if ch == nil {
		return snapshot, nil
	}
	if price, ok := ch.cachedModelPrices[actual]; ok {
		raw, err := json.Marshal(price.Price)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &snapshot.Price); err != nil {
			return nil, err
		}
		snapshot.PriceReference = price.ReferenceID
	}
	return snapshot, nil
}
