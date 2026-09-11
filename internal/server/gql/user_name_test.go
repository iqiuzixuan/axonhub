package gql

import (
	"context"
	"strings"
	"testing"

	"github.com/99designs/gqlgen/client"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestUserNameGraphQLRoundTrip(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:user-name-graphql?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(context.Background())
	owner := db.User.Create().SetEmail("name-owner@example.com").SetPassword("test-placeholder").
		SetIsOwner(true).SetName("原名称").SaveX(ctx)
	svc := biz.NewUserService(biz.UserServiceParams{Ent: db})
	h := NewGraphqlHandlers(Dependencies{Ent: db, UserService: svc})
	c := client.New(h.Graphql, func(req *client.Request) {
		requestCtx := ent.NewContext(authz.WithTestBypass(req.HTTP.Context()), db)
		req.HTTP = req.HTTP.WithContext(contexts.WithUser(requestCtx, owner))
	})

	var me struct{ Me struct{ Name string } }
	require.NoError(t, c.Post(`query { me { name } }`, &me))
	require.Equal(t, "原名称", me.Me.Name)
	var updated struct{ UpdateMe struct{ Name string } }
	require.NoError(t, c.Post(`mutation($input: UpdateMeInput!) { updateMe(input: $input) { name } }`, &updated,
		client.Var("input", map[string]any{"name": "  张 三  "})))
	require.Equal(t, "张 三", updated.UpdateMe.Name)
	require.NoError(t, c.Post(`query { me { name } }`, &me))
	require.Equal(t, "张 三", me.Me.Name)

	for _, invalid := range []string{" \t\u3000", strings.Repeat("张", 101)} {
		err := c.Post(`mutation($input: UpdateMeInput!) { updateMe(input: $input) { name } }`, &updated,
			client.Var("input", map[string]any{"name": invalid}))
		require.Error(t, err)
		require.Equal(t, "张 三", db.User.GetX(ctx, owner.ID).Name)
	}

	var created struct{ CreateUser struct{ ID, Name string } }
	require.NoError(t, c.Post(`mutation($input: CreateUserInput!) { createUser(input: $input) { id name } }`, &created,
		client.Var("input", map[string]any{"email": "name-user@example.com", "password": "test-password", "name": "小狐狸"})))
	require.Equal(t, "小狐狸", created.CreateUser.Name)
	var edited struct{ UpdateUser struct{ Name string } }
	require.NoError(t, c.Post(`mutation($id: ID!, $input: UpdateUserInput!) { updateUser(id: $id, input: $input) { name } }`, &edited,
		client.Var("id", created.CreateUser.ID), client.Var("input", map[string]any{"name": "李小明"})))
	require.Equal(t, "李小明", edited.UpdateUser.Name)

	var found struct {
		Users struct {
			Edges []struct{ Node struct{ Name string } }
		}
	}
	require.NoError(t, c.Post(`query { users(first: 10, where: {nameContainsFold: "李小"}) { edges { node { name } } } }`, &found))
	require.Len(t, found.Users.Edges, 1)
	require.Equal(t, "李小明", found.Users.Edges[0].Node.Name)

	// Split-name fields must not remain in the API after clients switch to name.
	require.Error(t, c.Post(`query { me { firstName lastName } }`, &me))

	// A migrated name can exceed the new-input limit. Editing unrelated fields
	// with name omitted must preserve that value, for both self and admin edits.
	legacyName := strings.Repeat("A", 50) + " " + strings.Repeat("B", 50)
	db.User.UpdateOneID(owner.ID).SetName(legacyName).SaveX(ctx)
	require.NoError(t, c.Post(`mutation($input: UpdateMeInput!) { updateMe(input: $input) { name } }`, &updated,
		client.Var("input", map[string]any{"preferLanguage": "zh"})))
	require.Equal(t, legacyName, updated.UpdateMe.Name)

	db.User.Update().Where(user.EmailEQ("name-user@example.com")).SetName(legacyName).SaveX(ctx)
	require.NoError(t, c.Post(`mutation($id: ID!, $input: UpdateUserInput!) { updateUser(id: $id, input: $input) { name } }`, &edited,
		client.Var("id", created.CreateUser.ID), client.Var("input", map[string]any{"email": "renamed-email@example.com"})))
	require.Equal(t, legacyName, edited.UpdateUser.Name)
}
