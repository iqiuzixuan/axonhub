package authz

import (
	"testing"

	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/role"
	"github.com/stretchr/testify/require"
)

func TestCanReadRequestDetails(t *testing.T) {
	for _, tc := range []struct {
		name      string
		user      *ent.User
		projectID int
		want      bool
	}{
		{name: "anonymous", projectID: 1},
		{name: "reader and writer", user: &ent.User{Scopes: []string{"read_requests", "write_requests"}}, projectID: 1},
		{name: "wildcard is not ownership", user: &ent.User{Scopes: []string{"*"}}, projectID: 1},
		{name: "system owner", user: &ent.User{IsOwner: true}, projectID: 2, want: true},
		{name: "owned project", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1, IsOwner: true}}}}, projectID: 1, want: true},
		{name: "other project", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1, IsOwner: true}}}}, projectID: 2},
		{name: "project scopes", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1, Scopes: []string{"*"}}}}}, projectID: 1},
		{name: "system administrator scopes", user: &ent.User{Scopes: []string{"read_requests", "write_users", "write_roles"}}, projectID: 2, want: true},
		{name: "system administrator role", user: &ent.User{Edges: ent.UserEdges{Roles: []*ent.Role{{Level: role.LevelSystem, Scopes: []string{"read_requests", "write_users", "write_roles"}}}}}, projectID: 2, want: true},
		{name: "project Admin role", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1}}, Roles: []*ent.Role{{Level: role.LevelProject, ProjectID: new(1), Scopes: []string{"read_requests", "write_users", "write_roles"}}}}}, projectID: 1, want: true},
		{name: "other project Admin role", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1}, {ProjectID: 2}}, Roles: []*ent.Role{{Level: role.LevelProject, ProjectID: new(1), Scopes: []string{"read_requests", "write_users", "write_roles"}}}}}, projectID: 2},
		{name: "role without membership", user: &ent.User{Edges: ent.UserEdges{Roles: []*ent.Role{{Level: role.LevelProject, ProjectID: new(1), Scopes: []string{"read_requests", "write_users", "write_roles"}}}}}, projectID: 1},
		{name: "cannot mix system and project grants", user: &ent.User{Scopes: []string{"write_users"}, Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1, Scopes: []string{"read_requests", "write_roles"}}}}}, projectID: 1},
		{name: "unknown resource project", user: &ent.User{Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: 1, IsOwner: true}}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := t.Context()
			if tc.user != nil {
				ctx = contexts.WithUser(ctx, tc.user)
			}
			ctx = contexts.WithProjectID(ctx, 1) // Forging the selected project never changes ownership.
			require.Equal(t, tc.want, CanReadRequestDetails(ctx, tc.projectID))
			if tc.want {
				require.NoError(t, RequireRequestDetails(ctx, tc.projectID))
			} else {
				require.Error(t, RequireRequestDetails(ctx, tc.projectID))
			}
		})
	}
}
