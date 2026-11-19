// 职责: 验证 GraphQL 渠道设置更新同时支持价格同步和渠道级响应设置。
// 关联文件:
//
//	internal/server/gql/axonhub.graphql
//	internal/server/biz/channel_settings.go

package gql

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

func TestChannelSettingsGraphQLUpdatesPriceSyncAndPreservesTimeouts(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:channel-settings-price-sync?mode=memory&_fk=1")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	service := biz.NewChannelServiceForTest(db)
	created, err := service.CreateChannel(ctx, ent.CreateChannelInput{
		Type:             channel.TypeOpenai,
		Name:             "Price sync fixture",
		Credentials:      objects.ChannelCredentials{APIKey: "fixture-key"},
		SupportedModels:  []string{"fixture-model"},
		DefaultTestModel: "fixture-model",
	})
	require.NoError(t, err)

	server := handler.NewDefaultServer(NewExecutableSchema(Config{Resolvers: &Resolver{channelService: service}}))
	const query = `mutation ($id: ID!, $input: UpdateChannelSettingsInput!) {
		updateChannelSettings(id: $id, input: $input) {
			settings { syncModelPrices maxSingleChannelRetries responseTimeout { mode nonStreamResponseTimeoutSeconds } }
		}
	}`
	for _, enabled := range []bool{true, false} {
		patch := map[string]any{"syncModelPrices": enabled}
		if enabled {
			patch["maxSingleChannelRetries"] = 2
			patch["responseTimeout"] = map[string]any{"mode": "CUSTOM", "nonStreamResponseTimeoutSeconds": 42}
		}
		id := fmt.Sprintf("gid://axonhub/Channel/%d", created.ID)
		payload, err := json.Marshal(map[string]any{
			"query": query, "variables": map[string]any{"id": id, "input": patch},
		})
		require.NoError(t, err)
		request := httptest.NewRequestWithContext(ctx, http.MethodPost, "/graphql", bytes.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		require.Equal(t, http.StatusOK, response.Code)
		expected := fmt.Sprintf(`{"data":{"updateChannelSettings":{"settings":{"syncModelPrices":%t,"maxSingleChannelRetries":2,"responseTimeout":{"mode":"CUSTOM","nonStreamResponseTimeoutSeconds":42}}}}}`, enabled)
		require.JSONEq(t, expected, response.Body.String())
		stored, err := db.Channel.Get(ctx, created.ID)
		require.NoError(t, err)
		require.Equal(t, enabled, stored.Settings.SyncModelPrices)
	}
}
