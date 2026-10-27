package gql

import (
	"context"
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/scopes"
)

// Statistics have dashboard access; loading user names still requires user access.
// Eager loading batches owners instead of issuing one query per API key.
func withAPIKeyOwner(ctx context.Context, query *ent.APIKeyQuery) *ent.APIKeyQuery {
	if authz.HasScope(ctx, scopes.ScopeReadUsers) {
		query.WithUser(func(q *ent.UserQuery) {
			q.Select(user.FieldID, user.FieldName)
		})
	}
	return query
}

func apiKeyUserName(key *ent.APIKey) *string {
	if key == nil || key.Edges.User == nil {
		return nil
	}
	name := strings.TrimSpace(key.Edges.User.Name)
	if name == "" {
		return nil
	}
	return lo.ToPtr(name)
}

// apiKeyOwnerNames looks up owner names for the dashboard's top API keys. It runs
// outside the shared dashboard cache because visibility depends on the caller's scopes.
func apiKeyOwnerNames(ctx context.Context, client *ent.Client, ids []int) (map[int]*string, error) {
	if len(ids) == 0 || !authz.HasScope(ctx, scopes.ScopeReadUsers) {
		return map[int]*string{}, nil
	}
	keys, err := withAPIKeyOwner(ctx, client.APIKey.Query().
		Where(apikey.IDIn(ids...))).
		All(ctx)
	if err != nil {
		return nil, err
	}
	return lo.SliceToMap(keys, func(key *ent.APIKey) (int, *string) {
		return key.ID, apiKeyUserName(key)
	}), nil
}
