package biz

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	entrequest "github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xcache"
	"github.com/looplj/axonhub/llm"
	"github.com/looplj/axonhub/llm/httpclient"
)

func TestRequestService_CreateRequestExecutionPersistsMaskedChannelAPIKey(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	t.Cleanup(func() { client.Close() })

	ctx := authz.WithTestBypass(ent.NewContext(context.Background(), client))
	systemService := NewSystemService(SystemServiceParams{Ent: client})
	channelService := NewChannelServiceForTest(client)
	dataStorageService := NewDataStorageService(DataStorageServiceParams{
		SystemService: systemService,
		CacheConfig:   xcache.Config{Mode: xcache.ModeMemory},
		Client:        client,
	})
	service := NewRequestService(client, systemService.CacheConfig, systemService,
		NewUsageLogService(client, systemService, channelService), dataStorageService, NewLiveStreamRegistry())

	keys := []string{"aaaa-secret-first-bbbb", "cccc-secret-second-dddd"}
	channelEntity, err := client.Channel.Create().
		SetName("multiple-keys").
		SetType(channel.TypeOpenai).
		SetBaseURL("https://example.invalid").
		SetCredentials(objects.ChannelCredentials{APIKeys: keys}).
		SetSupportedModels([]string{"test-model"}).
		SetDefaultTestModel("test-model").
		SetStatus(channel.StatusEnabled).
		Save(ctx)
	require.NoError(t, err)

	requestEntity, err := client.Request.Create().
		SetModelID("test-model").
		SetRequestBody([]byte(`{}`)).
		SetStatus(entrequest.StatusProcessing).
		Save(ctx)
	require.NoError(t, err)

	for _, storeBody := range []bool{true, false} {
		require.NoError(t, systemService.SetStoragePolicy(ctx, &StoragePolicy{StoreRequestBody: storeBody}))
		attemptCtx := contexts.WithChannelAPIKey(ctx, "")
		var executionIDs []int
		for _, key := range keys {
			// A retry may select another key on the same channel and shared context.
			attemptCtx = contexts.WithChannelAPIKey(attemptCtx, key)
			execution, err := service.CreateRequestExecution(attemptCtx, &Channel{Channel: channelEntity}, "test-model",
				requestEntity, httpclient.Request{
					Body:    []byte(`{"model":"test-model"}`),
					Headers: http.Header{"Authorization": {"Bearer " + key}},
				}, llm.APIFormatOpenAIChatCompletion, false)
			require.NoError(t, err)
			executionIDs = append(executionIDs, execution.ID)
			require.NotContains(t, string(execution.RequestHeaders), key)
			if !storeBody {
				require.JSONEq(t, `{}`, string(execution.RequestHeaders))
				require.JSONEq(t, `{}`, string(execution.RequestBody))
			}
		}

		// Read back after the retry to ensure each attempt retained its own snapshot.
		for i, want := range []string{"aaaa****bbbb", "cccc****dddd"} {
			execution, err := client.RequestExecution.Get(ctx, executionIDs[i])
			require.NoError(t, err)
			require.NotNil(t, execution.ChannelAPIKeyMasked)
			require.Equal(t, want, *execution.ChannelAPIKeyMasked)
		}
	}

	for _, key := range []string{"", "short", "12345678"} {
		execution, err := service.CreateRequestExecution(contexts.WithChannelAPIKey(ctx, key),
			&Channel{Channel: channelEntity}, "test-model", requestEntity,
			httpclient.Request{Body: []byte(`{}`)}, llm.APIFormatOpenAIChatCompletion, false)
		require.NoError(t, err)
		if key == "" {
			require.Nil(t, execution.ChannelAPIKeyMasked)
		} else {
			require.NotNil(t, execution.ChannelAPIKeyMasked)
			require.Equal(t, "****", *execution.ChannelAPIKeyMasked)
		}
	}
}
