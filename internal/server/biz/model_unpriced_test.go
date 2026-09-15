package biz

import (
	"testing"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/stretchr/testify/require"
)

func TestOriginalModelListIncludesUnpricedConfiguredModels(t *testing.T) {
	db := enttest.NewEntClient(t, "sqlite3", "file:unpriced-model-list?mode=memory&_fk=0")
	defer db.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), db))
	channel := db.Channel.Create().SetType("openai").SetName("provider").SetBaseURL("https://example.com/v1").
		SetCredentials(objects.ChannelCredentials{APIKey: "test"}).SetSupportedModels([]string{"internal-actual"}).
		SetDefaultTestModel("internal-actual").SetStatus("enabled").SaveX(ctx)
	channels := NewChannelServiceForTest(db)
	built, err := channels.buildChannelWithTransformer(channel)
	require.NoError(t, err)
	channels.SetEnabledChannelsForTest([]*Channel{built})
	system := NewSystemService(SystemServiceParams{Ent: db})
	require.NoError(t, system.SetModelSettings(ctx, SystemModelSettings{BillingModelSource: objects.BillingModelSourceOriginal, QueryAllChannelModels: true}))
	db.Model.Create().SetModelID("codex-auto-review").SetName("Review").SetDeveloper("custom").SetIcon("").SetGroup("").
		SetStatus("enabled").SetModelCard(&objects.ModelCard{}).SetSettings(&objects.ModelSettings{
		Associations: []*objects.ModelAssociation{{Type: "model", ModelID: &objects.ModelIDAssociation{ModelID: "internal-actual"}}},
	}).SaveX(ctx)
	service := NewModelService(ModelServiceParams{Ent: db, ChannelService: channels, SystemService: system})
	models, err := service.ListEnabledModels(ctx)
	require.NoError(t, err)
	require.Len(t, models, 1)
	require.Equal(t, "codex-auto-review", models[0].ID, "missing price must not hide a configured public model")
	// Raw channel models remain private even when QueryAllChannelModels is true.
	require.NotEqual(t, "internal-actual", models[0].ID)
}
