package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/role"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestUserNameHTTP_InitializationAndInvitation(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	systemService := biz.NewSystemService(biz.SystemServiceParams{Ent: client, CacheConfig: xcache.Config{Mode: xcache.ModeMemory}})
	invitationService := biz.NewInvitationService(biz.InvitationServiceParams{Ent: client})
	systemHandlers := NewSystemHandlers(SystemHandlersParams{SystemService: systemService})
	invitationHandlers := NewInvitationHandlers(InvitationHandlersParams{
		InvitationService: invitationService,
		AuthService:       biz.NewAuthService(biz.AuthServiceParams{Ent: client, SystemService: systemService}),
	})
	router := gin.New()
	router.POST("/initialize", systemHandlers.InitializeSystem)
	router.POST("/invitation/:token/register", invitationHandlers.Register)

	initialize := map[string]any{
		"ownerEmail": "owner@example.com", "ownerPassword": "password123", "brandName": "Test",
	}
	for _, name := range []string{"", " \t　", strings.Repeat("张", 101)} {
		initialize["ownerName"] = name
		response := postNameJSON(t, router, ctx, "/initialize", initialize)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	initialize["ownerName"] = "  张三  "
	response := postNameJSON(t, router, ctx, "/initialize", initialize)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	owner, err := client.User.Query().Where(user.EmailEQ("owner@example.com")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "张三", owner.Name)

	project, err := client.Project.Query().Only(ctx)
	require.NoError(t, err)
	projectRole, err := client.Role.Query().Where(role.NameEQ("Developer"), role.ProjectIDEQ(project.ID)).Only(ctx)
	require.NoError(t, err)
	created, err := invitationService.CreateInvitation(contexts.WithUser(ctx, owner), project.ID, projectRole.ID, nil, 1)
	require.NoError(t, err)
	registration := map[string]any{"email": "member@example.com", "password": "password123"}
	path := "/invitation/" + created.Token + "/register"
	for _, name := range []string{"", " \t　", strings.Repeat("张", 101)} {
		registration["name"] = name
		response := postNameJSON(t, router, ctx, path, registration)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	registration["name"] = "  李小明 的昵称  "
	response = postNameJSON(t, router, ctx, path, registration)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var payload struct {
		User  map[string]any `json:"user"`
		Token string         `json:"token"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &payload))
	require.Equal(t, "李小明 的昵称", payload.User["name"])
	require.NotContains(t, payload.User, "firstName")
	require.NotContains(t, payload.User, "lastName")
	require.NotEmpty(t, payload.Token)
	member, err := client.User.Query().Where(user.EmailEQ("member@example.com")).Only(ctx)
	require.NoError(t, err)
	require.Equal(t, "李小明 的昵称", member.Name)
}

func postNameJSON(t *testing.T, handler http.Handler, ctx context.Context, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
