package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestRequestDetailEndpointsDenyNonOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := enttest.NewEntClient(t, "sqlite3", "file:request-detail-endpoints?mode=memory&_fk=0")
	defer db.Close()
	setup := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	project := db.Project.Create().SetName("protected").SaveX(setup)
	req := db.Request.Create().SetProjectID(project.ID).SetModelID("test").SetStatus("completed").SetSource("api").SetRequestBody(objects.JSONRawMessage(`{}`)).SaveX(setup)
	for _, ownerOfOther := range []bool{false, true} {
		for _, endpoint := range []string{"content", "preview"} {
			t.Run(fmt.Sprintf("%s/other-owner=%t", endpoint, ownerOfOther), func(t *testing.T) {
				router := gin.New()
				router.Use(func(c *gin.Context) {
					viewer := &ent.User{ID: 1, Scopes: []string{"read_requests"}, Edges: ent.UserEdges{ProjectUsers: []*ent.UserProject{{ProjectID: project.ID + 1, IsOwner: ownerOfOther}}}}
					ctx := authz.NewUserContext(ent.NewContext(c.Request.Context(), db), viewer.ID)
					ctx = contexts.WithUser(ctx, viewer)
					ctx = contexts.WithProjectID(ctx, project.ID)
					c.Request = c.Request.WithContext(ctx)
					c.Next()
				})
				router.GET("/requests/:request_id/content", (&RequestContentHandlers{}).DownloadRequestContent)
				router.GET("/requests/:request_id/preview", (&RequestPreviewHandlers{}).PreviewRequest)
				recorder := httptest.NewRecorder()
				router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, fmt.Sprintf("/requests/%d/%s", req.ID, endpoint), nil))
				require.Equal(t, http.StatusForbidden, recorder.Code, recorder.Body.String())
			})
		}
	}
}
