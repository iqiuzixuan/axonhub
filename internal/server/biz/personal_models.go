package biz

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/model"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xerrors"
	"github.com/samber/lo"
)

// PersonalModel is intentionally not an Ent Model: settings, associations,
// channel counts and graph edges cannot be selected through this query.
type PersonalModel struct {
	ID        string
	ModelID   string
	Name      string
	Developer string
	Icon      string
	Group     string
	Type      *model.Type
	Status    model.Status
	CreatedAt time.Time
	UpdatedAt time.Time
	ModelCard *objects.ModelCard
	APIKeys   []*PersonalAPIKey
}

func (s *PersonalService) Models(ctx context.Context, scope PersonalScope, search string) ([]*PersonalModel, error) {
	keys, err := s.personalKeys(ctx, scope)
	if err != nil {
		return nil, err
	}
	if utf8.RuneCountInString(search) > 200 {
		return nil, xerrors.ValidationError("model search cannot exceed 200 characters")
	}
	result := make([]*PersonalModel, 0)
	byID := make(map[string]*PersonalModel)
	search = strings.ToLower(strings.TrimSpace(search))
	err = authz.RunWithSystemBypassVoid(ctx, "personal-model-catalog", func(readCtx context.Context) error {
		for _, key := range keys {
			if !personalKeyCanCall(key) {
				continue
			}
			// Explicit key argument avoids mutating the shared contexts container
			// while sibling GraphQL fields are executing.
			facades, err := s.models.listEnabledModels(readCtx, key, true)
			if err != nil {
				return err
			}
			for _, facade := range facades {
				item, exists := byID[facade.ID]
				if !exists {
					item = &PersonalModel{
						ID: facade.ID, ModelID: facade.ID, Name: facade.DisplayName,
						Developer: facade.OwnedBy, Status: model.StatusEnabled,
						CreatedAt: facade.CreatedAt, UpdatedAt: facade.CreatedAt, APIKeys: []*PersonalAPIKey{},
					}
					byID[facade.ID] = item
				}
				item.APIKeys = append(item.APIKeys, personalKeyInfo(key))
			}
		}
		if len(byID) == 0 {
			return nil
		}
		ids := make([]string, 0, len(byID))
		for id := range byID {
			ids = append(ids, id)
		}
		configured, err := s.entFromContext(ctx).Model.Query().Where(model.ModelIDIn(ids...), model.StatusEQ(model.StatusEnabled)).All(readCtx)
		if err != nil {
			return err
		}
		for _, entity := range configured {
			item := byID[entity.ModelID]
			item.Name, item.Developer, item.Icon, item.Group = entity.Name, entity.Developer, entity.Icon, entity.Group
			item.Type, item.ModelCard = lo.ToPtr(entity.Type), entity.ModelCard
			item.CreatedAt, item.UpdatedAt = entity.CreatedAt, entity.UpdatedAt
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("load personal models: %w", err)
	}
	for _, item := range byID {
		if search == "" || strings.Contains(strings.ToLower(item.Name+" "+item.ModelID), search) {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ModelID < result[j].ModelID })
	return result, nil
}
