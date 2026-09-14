package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/channel"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
)

func TestNewEntClientUpgradesChannelAutoDisableExpiry(t *testing.T) {
	dsn := "file:" + filepath.Join(t.TempDir(), "legacy-channels.db")
	ctx := authz.WithTestBypass(context.Background())
	legacy := enttest.NewEntClient(t, "sqlite3", dsn)
	disabledAt := time.Now().UTC().Truncate(time.Second)
	ch := legacy.Channel.Create().
		SetType(channel.TypeOpenai).
		SetName("Existing channel").
		SetBaseURL("https://example.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "migration-test-placeholder"}).
		SetSupportedModels([]string{"test-model"}).
		SetDefaultTestModel("test-model").
		SetStatus(channel.StatusDisabled).
		SetAutoDisabledAt(disabledAt).
		SaveX(ctx)
	require.NoError(t, legacy.Close())

	// Reproduce the channel schema before upstream introduced recovery expiry.
	raw, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)
	_, err = raw.ExecContext(ctx, "ALTER TABLE channels DROP COLUMN auto_disable_expires_at")
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	upgraded := NewEntClient(Config{Dialect: "sqlite3", DSN: dsn, MaxOpenConns: 1, MaxIdleConns: 1})
	got := upgraded.Channel.GetX(ctx, ch.ID)
	require.Equal(t, ch.Name, got.Name)
	require.Equal(t, channel.StatusDisabled, got.Status)
	require.NotNil(t, got.AutoDisabledAt)
	require.True(t, disabledAt.Equal(*got.AutoDisabledAt))
	require.Nil(t, got.AutoDisableExpiresAt)

	expiresAt := disabledAt.Add(time.Hour)
	upgraded.Channel.UpdateOne(got).SetAutoDisableExpiresAt(expiresAt).SaveX(ctx)
	require.NoError(t, upgraded.Close())

	// A repeated startup must preserve a newly scheduled recovery.
	reopened := NewEntClient(Config{Dialect: "sqlite3", DSN: dsn, MaxOpenConns: 1, MaxIdleConns: 1})
	defer reopened.Close()
	got = reopened.Channel.GetX(ctx, ch.ID)
	require.NotNil(t, got.AutoDisableExpiresAt)
	require.True(t, expiresAt.Equal(*got.AutoDisableExpiresAt))
	require.Equal(t, channel.StatusDisabled, got.Status)
}
