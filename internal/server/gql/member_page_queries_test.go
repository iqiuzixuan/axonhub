package gql

import (
	"fmt"
	"os"
	"regexp"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func memberPageFrontendQuery(t *testing.T, path, name string) string {
	t.Helper()
	source, err := os.ReadFile("../../../frontend/src/" + path)
	require.NoError(t, err)
	match := regexp.MustCompile("(?s)const " + regexp.QuoteMeta(name) + " = `([^`]+)`").FindSubmatch(source)
	require.Len(t, match, 2)
	return string(match[1])
}

func TestMemberPageQueries(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer db.Close()
	setupCtx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	project := db.Project.Create().SetName("member-project").SaveX(setupCtx)
	otherProject := db.Project.Create().SetName("other-project").SaveX(setupCtx)
	member := db.User.Create().SetEmail("member@example.com").SetName("Member").SetPassword("test").SaveX(setupCtx)
	peer := db.User.Create().SetEmail("peer@example.com").SetName("Peer").SetPassword("test").SaveX(setupCtx)
	outsider := db.User.Create().SetEmail("outside@example.com").SetName("Outside").SetPassword("test").SaveX(setupCtx)
	db.UserProject.Create().SetUserID(member.ID).SetProjectID(project.ID).SetScopes([]string{"read_api_keys", "read_users"}).SaveX(setupCtx)
	db.UserProject.Create().SetUserID(peer.ID).SetProjectID(project.ID).SaveX(setupCtx)
	db.UserProject.Create().SetUserID(outsider.ID).SetProjectID(otherProject.ID).SaveX(setupCtx)
	role := db.Role.Create().SetName("Member role").SetLevel("project").SetProjectID(project.ID).SaveX(setupCtx)
	db.User.UpdateOne(member).AddRoleIDs(role.ID).SaveX(setupCtx)
	db.APIKey.Create().SetName("default").SetKey("test-member-key").SetType("personal").SetUserID(member.ID).SetProjectID(project.ID).SaveX(setupCtx)
	member = db.User.Query().Where(user.IDEQ(member.ID)).WithProjectUsers().WithRoles().OnlyX(setupCtx)

	systemService := biz.NewSystemService(biz.SystemServiceParams{Ent: db, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	require.NoError(t, systemService.SetBrandName(setupCtx, "Company Brand"))
	require.NoError(t, systemService.SetBrandLogo(setupCtx, "data:image/png;base64,dGVzdA=="))
	require.NoError(t, systemService.SetTitle(setupCtx, "Company Gateway"))
	h := NewGraphqlHandlers(Dependencies{Ent: db, SystemService: systemService})
	c := client.New(h.Graphql, func(req *client.Request) {
		// Requests deliberately use a regular user principal without the setup bypass.
		ctx := authz.NewUserContext(ent.NewContext(req.HTTP.Context(), db), member.ID)
		ctx = contexts.WithProjectID(contexts.WithUser(ctx, member), project.ID)
		req.HTTP = req.HTTP.WithContext(ctx)
	})

	t.Run("API keys load while the old creator query is denied by role privacy", func(t *testing.T) {
		var keys struct {
			APIKeys struct {
				Edges []struct{ Node struct{ Name string } }
			}
		}
		require.NoError(t, c.Post(`query { apiKeys(first: 100) { edges { node { name } } } }`, &keys))
		require.Len(t, keys.APIKeys.Edges, 1)
		var users any
		err := c.Post(memberPageFrontendQuery(t, "gql/users.ts", "USERS_QUERY"), &users, client.Var("first", 100))
		require.ErrorContains(t, err, "permission denied")
	})

	t.Run("creator options need no role permission and stay in the current project", func(t *testing.T) {
		var result struct {
			Users struct {
				Edges []struct {
					Node struct{ ID, Name, Email string }
				}
			}
		}
		query := memberPageFrontendQuery(t, "features/apikeys/data/creator-options.ts", "API_KEY_CREATOR_OPTIONS_QUERY")
		require.NoError(t, c.Post(query, &result, client.Var("where", map[string]any{
			"hasProjectsWith": []map[string]any{{"id": fmt.Sprintf("gid://axonhub/Project/%d", project.ID)}},
		})))
		names := make([]string, 0, len(result.Users.Edges))
		for _, edge := range result.Users.Edges {
			names = append(names, edge.Node.Name)
		}
		require.ElementsMatch(t, []string{"Member", "Peer"}, names)
	})

	t.Run("brand display is readable without system settings permission", func(t *testing.T) {
		var result struct {
			BrandSettings struct{ BrandName, BrandLogo, Title string }
		}
		require.NoError(t, c.Post(memberPageFrontendQuery(t, "features/system/data/system.ts", "BRAND_SETTINGS_QUERY"), &result))
		require.Equal(t, "Company Brand", result.BrandSettings.BrandName)
		require.Equal(t, "data:image/png;base64,dGVzdA==", result.BrandSettings.BrandLogo)
		require.Equal(t, "Company Gateway", result.BrandSettings.Title)
		var mutation any
		require.ErrorContains(t, c.Post(`mutation { updateBrandSettings(input: {brandName: "unauthorized"}) }`, &mutation), "permission denied")
	})
}
