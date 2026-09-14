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
	"github.com/looplj/axonhub/internal/ent/role"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestRequestDetailsAuthorization(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:request-detail-auth?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	p := db.Project.Create().SetName("owned").SaveX(setup)
	other := db.Project.Create().SetName("other").SaveX(setup)
	req := db.Request.Create().SetProjectID(p.ID).SetModelID("test-model").SetStatus("completed").SetSource("api").
		SetRequestBody(objects.JSONRawMessage(`{"secret":"body"}`)).SetResponseBody(objects.JSONRawMessage(`{"secret":"response"}`)).SetRequestHeaders(objects.JSONRawMessage(`{"secret":"headers"}`)).SaveX(setup)
	execution := db.RequestExecution.Create().SetProjectID(p.ID).SetRequestID(req.ID).SetChannelID(1).SetModelID("test-model").
		SetStatus("completed").SetFormat("openai/chat_completions").SetRequestBody(objects.JSONRawMessage(`{}`)).
		SetRequestHeaders(objects.JSONRawMessage(`{"secret":"execution"}`)).SetErrorMessage("secret-error").SaveX(setup)
	thread := db.Thread.Create().SetProjectID(p.ID).SetThreadID("test-thread").SaveX(setup)
	trace := db.Trace.Create().SetProjectID(p.ID).SetTraceID("test-trace").SetThreadID(thread.ID).SaveX(setup)
	db.DataStorage.Create().SetName("primary").SetDescription("Test database storage").SetType("database").SetPrimary(true).SetSettings(&objects.DataStorageSettings{}).SaveX(setup)
	storage := &biz.DataStorageService{Cache: xcache.NewFromConfig[ent.DataStorage](xcache.Config{Mode: xcache.ModeMemory})}
	requests := &biz.RequestService{DataStorageService: storage, LiveStreamRegistry: biz.NewLiveStreamRegistry()}
	h := NewGraphqlHandlers(Dependencies{Ent: db, RequestService: requests})
	for _, tc := range []struct {
		name         string
		systemOwner  bool
		adminProject int
		systemAdmin  bool
		ownedProject int
		allowed      bool
	}{
		{name: "ordinary member"},
		{name: "system admin role", systemAdmin: true, allowed: true},
		{name: "project Admin role", adminProject: p.ID, allowed: true},
		{name: "other project Admin role", adminProject: other.ID},
		{name: "system owner", systemOwner: true, allowed: true},
		{name: "project owner", ownedProject: p.ID, allowed: true},
		{name: "other project owner", ownedProject: other.ID},
	} {
		t.Run(tc.name, func(t *testing.T) {
			viewer := &ent.User{ID: 1, IsOwner: tc.systemOwner, Scopes: []string{"read_requests", "write_requests"}}
			viewer.Edges.ProjectUsers = []*ent.UserProject{{ProjectID: p.ID, IsOwner: tc.ownedProject == p.ID, Scopes: []string{"read_requests"}}, {ProjectID: other.ID, IsOwner: tc.ownedProject == other.ID}}
			if tc.adminProject != 0 {
				viewer.Edges.Roles = []*ent.Role{{Level: role.LevelProject, ProjectID: new(tc.adminProject), Scopes: []string{"read_requests", "write_users", "write_roles"}}}
			}
			if tc.systemAdmin {
				viewer.Edges.Roles = []*ent.Role{{Level: role.LevelSystem, Scopes: []string{"read_requests", "write_users", "write_roles"}}}
			}

			gql := client.New(h.Graphql, func(req *client.Request) {
				ctx := authz.NewUserContext(ent.NewContext(req.HTTP.Context(), db), viewer.ID)
				ctx = contexts.WithUser(ctx, viewer)
				ctx = contexts.WithProjectID(ctx, p.ID)
				req.HTTP = req.HTTP.WithContext(ctx)
			})
			var summary struct {
				Requests struct {
					Edges []struct {
						Node struct {
							ID         string
							ModelID    string
							Status     string
							Executions map[string]any
							UsageLogs  map[string]any
						}
					}
				}
			}
			require.NoError(t, gql.Post(`{ requests(first:10) { edges { node { id modelID status executions(first:1) { edges { node { id modelID status } } } usageLogs(first:1) { edges { node { totalTokens } } } } } } }`, &summary))
			require.Len(t, summary.Requests.Edges, 1)
			require.Equal(t, "test-model", summary.Requests.Edges[0].Node.ModelID)
			for _, query := range []string{
				fmt.Sprintf(`{ node(id:"gid://axonhub/Request/%d") { ... on Request { renamed: requestHeaders } } }`, req.ID),
				`{ requests(first:10) { edges { node { requestHeaders } } } }`,
				fmt.Sprintf(`{ node(id:"gid://axonhub/Request/%d") { ... on Request { id status requestBody responseBody } } }`, req.ID),
				`{ requests(first:10) { edges { node { executions(first:1) { edges { node { requestHeaders errorMessage } } } } } } }`,
				fmt.Sprintf(`{ node(id:"gid://axonhub/RequestExecution/%d") { ... on RequestExecution { requestHeaders errorMessage } } }`, execution.ID),
			} {
				var result map[string]any
				err := gql.Post(query, &result)
				if tc.allowed {
					require.NoError(t, err, query)
					require.Contains(t, fmt.Sprint(result), "secret")
				} else {
					require.ErrorContains(t, err, "permission denied", query)
					require.NotContains(t, fmt.Sprint(result), "secret")
				}
			}
			if !tc.allowed {
				for _, field := range []string{"requestBody", "responseBody", "responseChunks", "contentStorageKey"} {
					var result map[string]any
					require.ErrorContains(t, gql.Post(fmt.Sprintf(`{ node(id:"gid://axonhub/Request/%d") { ... on Request { %s } } }`, req.ID, field), &result), "permission denied")
				}
				for _, field := range []string{"rootSegment { id }", "rawRootSegment", "firstText"} {
					var result map[string]any
					require.ErrorContains(t, gql.Post(fmt.Sprintf(`{ node(id:"gid://axonhub/Trace/%d") { ... on Trace { %s } } }`, trace.ID, field), &result), "permission denied")
				}
				var previews struct {
					Threads struct {
						Edges []struct {
							Node struct{ FirstUserQuery *string }
						}
					}
					Traces struct {
						Edges []struct {
							Node struct{ FirstUserQuery *string }
						}
					}
				}
				require.NoError(t, gql.Post(`{ threads(first:10) { edges { node { firstUserQuery } } } traces(first:10) { edges { node { firstUserQuery } } } }`, &previews))
				require.Len(t, previews.Threads.Edges, 1)
				require.Nil(t, previews.Threads.Edges[0].Node.FirstUserQuery)
				require.Len(t, previews.Traces.Edges, 1)
				require.Nil(t, previews.Traces.Edges[0].Node.FirstUserQuery)
			}
		})
	}
}
