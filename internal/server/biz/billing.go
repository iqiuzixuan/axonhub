package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/objects"
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

// PrepareBilling freezes the policy before routing. Prices are captured from the
// selected channel immediately before each outbound attempt.
func (s *UsageLogService) PrepareBilling(ctx context.Context, original string) (*objects.RequestBilling, error) {
	source, err := s.SystemService.BillingModelSource(ctx)
	if err != nil {
		return nil, fmt.Errorf("load billing policy: %w", err)
	}
	snapshot := &objects.RequestBilling{Source: source, OriginalModel: original, At: time.Now()}
	snapshot.InjectCost, _ = s.SystemService.InjectUsageCostEnabled(ctx)
	return snapshot, nil
}

// SnapshotRequestPrices uses the same channel lookup for both billing modes.
// Missing prices stay missing; neither another channel nor another model is used.
// The actual model's price is retained separately for provider cost accounting.
func (s *UsageLogService) SnapshotRequestPrices(policy *objects.RequestBilling, channelID int, actual string) (*objects.RequestBilling, *objects.RequestBilling, error) {
	cost, err := s.SnapshotChannelPrice(channelID, actual)
	if err != nil || policy == nil {
		return nil, cost, err
	}
	price := cost
	if policy.Source == objects.BillingModelSourceOriginal && policy.OriginalModel != actual {
		price, err = s.SnapshotChannelPrice(channelID, policy.OriginalModel)
		if err != nil {
			return nil, nil, err
		}
	}
	billing := *price
	billing.Source = policy.Source
	billing.OriginalModel = policy.OriginalModel
	billing.InjectCost = policy.InjectCost
	return &billing, cost, nil
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
	if req.Billing != nil {
		_, usage.Cost, _ = requestBillingCost(req.Billing, usage)
		return
	}
	if execution.CostPrice != nil {
		_, usage.Cost, _ = requestBillingCost(execution.CostPrice, usage)
		return
	}
	s.InjectUsageCost(ctx, execution.ChannelID, execution.ModelID, usage)
}

// SnapshotChannelPrice freezes the selected channel's price for an exact model ID.
func (s *UsageLogService) SnapshotChannelPrice(channelID int, modelID string) (*objects.RequestBilling, error) {
	snapshot := &objects.RequestBilling{Source: objects.BillingModelSourceRedirected, OriginalModel: modelID, At: time.Now()}
	ch := s.ChannelService.GetEnabledChannel(channelID)
	if ch == nil {
		return snapshot, nil
	}
	if price, ok := ch.cachedModelPrices[modelID]; ok {
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
