package gql

import (
	"context"
	"strings"

	"github.com/samber/lo"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
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
