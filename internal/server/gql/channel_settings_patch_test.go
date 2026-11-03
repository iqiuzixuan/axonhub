package gql

import (
	"testing"

	"github.com/looplj/axonhub/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestChannelSettingsPatchRetryOverridePresence(t *testing.T) {
	ec := &executionContext{}
	for _, value := range []int{0, 3, 65535} {
		input, err := ec.unmarshalInputUpdateChannelSettingsInput(t.Context(), map[string]any{
			"maxSingleChannelRetries": value,
		})
		require.NoError(t, err)
		require.NotNil(t, input.MaxSingleChannelRetries)
		require.Equal(t, value, *input.MaxSingleChannelRetries)
	}
	input, err := ec.unmarshalInputUpdateChannelSettingsInput(t.Context(), map[string]any{})
	require.NoError(t, err)
	require.Nil(t, input.MaxSingleChannelRetries)
}

func TestChannelSettingsPatchResponseTimeout(t *testing.T) {
	ec := &executionContext{}
	input, err := ec.unmarshalInputUpdateChannelSettingsInput(t.Context(), map[string]any{
		"responseTimeout": map[string]any{
			"mode": "CUSTOM", "streamFirstEventTimeoutSeconds": 0, "nonStreamResponseTimeoutSeconds": 45,
		},
	})
	require.NoError(t, err)
	require.NotNil(t, input.ResponseTimeout)
	require.Equal(t, objects.ChannelResponseTimeoutModeCustom, input.ResponseTimeout.Mode)
	require.Equal(t, 0, *input.ResponseTimeout.StreamFirstEventTimeoutSeconds)
	require.Equal(t, 45, *input.ResponseTimeout.NonStreamResponseTimeoutSeconds)
	input, err = ec.unmarshalInputUpdateChannelSettingsInput(t.Context(), map[string]any{})
	require.NoError(t, err)
	require.Nil(t, input.ResponseTimeout)
}
