package authz

import (
	"context"
	"slices"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent/privacy"
	"github.com/looplj/axonhub/internal/scopes"
)

// CanReadRequestDetails checks administration of the resource's project, never
// a project ID supplied by the caller. Request read/write scopes alone do not
// grant access. Administrators can manage both members and roles at that level.
func CanReadRequestDetails(ctx context.Context, projectID int) bool {
	user, ok := contexts.GetUser(ctx)
	if !ok || user == nil {
		return false
	}
	if user.IsOwner {
		return true
	}
	systemScopes := slices.Clone(user.Scopes)
	for _, role := range user.Edges.Roles {
		if role.IsSystemRole() {
			systemScopes = append(systemScopes, role.Scopes...)
		}
	}
	if hasRequestAdministration(systemScopes) {
		return true
	}
	if projectID <= 0 {
		return false
	}
	for _, membership := range user.Edges.ProjectUsers {
		if membership.ProjectID != projectID {
			continue
		}
		if membership.IsOwner {
			return true
		}
		projectScopes := slices.Clone(membership.Scopes)
		for _, role := range user.Edges.Roles {
			if !role.IsSystemRole() && role.ProjectID != nil && *role.ProjectID == projectID {
				projectScopes = append(projectScopes, role.Scopes...)
			}
		}
		return hasRequestAdministration(projectScopes)
	}
	return false
}

func hasRequestAdministration(grants []string) bool {
	return slices.Contains(grants, string(scopes.ScopeReadRequests)) &&
		slices.Contains(grants, string(scopes.ScopeWriteUsers)) &&
		slices.Contains(grants, string(scopes.ScopeWriteRoles))
}

func RequireRequestDetails(ctx context.Context, projectID int) error {
	if !CanReadRequestDetails(ctx, projectID) {
		return privacy.Denyf("request details require system or project administration")
	}
	return nil
}
