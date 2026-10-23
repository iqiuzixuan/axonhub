package gql

import (
	"testing"

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
