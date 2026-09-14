package gql

import (
	"fmt"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/user"
)

func TestAPIKeySearch_OwnerNameAndPagination(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:api-key-search?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	project := db.Project.Create().SetName("search-project").SaveX(setup)
	otherProject := db.Project.Create().SetName("other-project").SaveX(setup)
	member := db.User.Create().SetEmail("member@example.test").SetName("Member").SetPassword("test").SaveX(setup)
	owner := db.User.Create().SetEmail("owner@example.test").SetName("张 三 Alex").SetPassword("test").SaveX(setup)
	db.UserProject.Create().SetUserID(member.ID).SetProjectID(project.ID).SetScopes([]string{"read_api_keys", "read_users"}).SaveX(setup)
	db.UserProject.Create().SetUserID(owner.ID).SetProjectID(project.ID).SaveX(setup)
	member = db.User.Query().Where(user.IDEQ(member.ID)).WithProjectUsers().OnlyX(setup)

	// Enough unrelated keys to place the owner's matches beyond the first page.
	for i := range 101 {
		db.APIKey.Create().SetName(fmt.Sprintf("a-unrelated-%03d", i)).SetKey(fmt.Sprintf("test-only-%d", i)).
			SetProjectID(project.ID).SetUserID(member.ID).SaveX(setup)
	}
	for _, name := range []string{"first-match", "second-match"} {
		db.APIKey.Create().SetName(name).SetKey("test-" + name).SetProjectID(project.ID).SetUserID(owner.ID).SaveX(setup)
	}
	db.APIKey.Create().SetName("archived-match").SetKey("test-archived").SetStatus("archived").SetProjectID(project.ID).SetUserID(owner.ID).SaveX(setup)
	db.APIKey.Create().SetName("private-match").SetKey("test-personal").SetType("personal").SetProjectID(project.ID).SetUserID(owner.ID).SaveX(setup)
	db.APIKey.Create().SetName("outside-match").SetKey("test-outside").SetProjectID(otherProject.ID).SetUserID(owner.ID).SaveX(setup)

	h := NewGraphqlHandlers(Dependencies{Ent: db})
	c := client.New(h.Graphql, func(req *client.Request) {
		ctx := authz.NewUserContext(ent.NewContext(req.HTTP.Context(), db), member.ID)
		req.HTTP = req.HTTP.WithContext(contexts.WithProjectID(contexts.WithUser(ctx, member), project.ID))
	})
	const query = `query SearchAPIKeys($where: APIKeyWhereInput!, $after: Cursor) {
		apiKeys(first: 1, after: $after, orderBy: {field: NAME, direction: ASC}, where: $where) {
			edges { node { name user { name } } }
			totalCount pageInfo { hasNextPage endCursor }
		}
	}`
	type result struct {
		APIKeys struct {
			Edges []struct {
				Node struct {
					Name string
					User *struct{ Name string }
				}
			}
			TotalCount int
			PageInfo   struct {
				HasNextPage bool
				EndCursor   string
			}
		}
	}
	searchWhere := func(term string) map[string]any {
		return map[string]any{
			"typeNotIn": []string{"noauth"}, "statusIn": []string{"enabled", "disabled"},
			"or": []map[string]any{
				{"nameContainsFold": term},
				{"keyContainsFold": term},
				{"hasUserWith": []map[string]any{{"nameContainsFold": term}}},
			},
		}
	}
	for _, term := range []string{"张 三", "aLeX"} {
		t.Run(term, func(t *testing.T) {
			var first, next result
			require.NoError(t, c.Post(query, &first, client.Var("where", searchWhere(term))))
			require.Equal(t, 2, first.APIKeys.TotalCount)
			require.Len(t, first.APIKeys.Edges, 1)
			require.Equal(t, "first-match", first.APIKeys.Edges[0].Node.Name)
			require.NotNil(t, first.APIKeys.Edges[0].Node.User)
			require.Equal(t, "张 三 Alex", first.APIKeys.Edges[0].Node.User.Name)
			require.True(t, first.APIKeys.PageInfo.HasNextPage)
			require.NoError(t, c.Post(query, &next, client.Var("where", searchWhere(term)), client.Var("after", first.APIKeys.PageInfo.EndCursor)))
			require.Equal(t, "second-match", next.APIKeys.Edges[0].Node.Name)
			require.False(t, next.APIKeys.PageInfo.HasNextPage)
		})
	}
	var byName, byKey, archived result
	require.NoError(t, c.Post(query, &byName, client.Var("where", searchWhere("A-UNRELATED-000"))))
	require.Equal(t, 1, byName.APIKeys.TotalCount)
	require.NoError(t, c.Post(query, &byKey, client.Var("where", searchWhere("test-first-match"))))
	require.Equal(t, 1, byKey.APIKeys.TotalCount)
	where := searchWhere("Alex")
	where["statusIn"] = []string{"archived"}
	require.NoError(t, c.Post(query, &archived, client.Var("where", where)))
	require.Equal(t, 1, archived.APIKeys.TotalCount)
	require.Equal(t, "archived-match", archived.APIKeys.Edges[0].Node.Name)
}
