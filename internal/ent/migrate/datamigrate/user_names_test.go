package datamigrate_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/migrate/datamigrate"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
)

func TestBackfillUserNames(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:user-name-backfill?mode=memory&_fk=0")
	defer client.Close()
	ctx := schematype.SkipSoftDelete(authz.WithTestBypass(context.Background()))

	for i, tc := range []struct{ first, last, name, want string }{
		{" 三 ", " 张 ", "", "张三"},
		{"John", "Smith", "", "John Smith"},
		{"小明", "", "", "小明"},
		{"", "张", "", "张"},
		{"", "", "", "user-4@example.com"},
		{"三", "张", "小狐狸", "小狐狸"},
		{strings.Repeat("长", 120), "名", "", "名" + strings.Repeat("长", 120)},
	} {
		u := client.User.Create().SetEmail(fmt.Sprintf("user-%d@example.com", i)).
			SetPassword("migration-test-placeholder").SetFirstName(tc.first).
			SetLastName(tc.last).SetName(tc.name).SaveX(ctx)
		// Include users retained by soft deletion, not only active accounts.
		if i == 1 {
			client.User.UpdateOne(u).SetDeletedAt(123).SaveX(ctx)
		}
		require.NoError(t, datamigrate.BackfillUserNames(ctx, client))
		got := client.User.GetX(ctx, u.ID)
		require.Equal(t, tc.want, got.Name)
		require.Equal(t, tc.first, got.FirstName)
		require.Equal(t, tc.last, got.LastName)
	}

	u := client.User.Query().FirstX(ctx)
	client.User.UpdateOne(u).SetName("我的新昵称").SaveX(ctx)
	require.NoError(t, datamigrate.BackfillUserNames(ctx, client))
	require.Equal(t, "我的新昵称", client.User.GetX(ctx, u.ID).Name)
}

func TestBackfillUserNamesAcrossBatches(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:user-name-batches?mode=memory&_fk=0")
	defer client.Close()
	ctx := authz.WithTestBypass(context.Background())
	for i := range 205 {
		client.User.Create().SetEmail(fmt.Sprintf("batch-%d@example.com", i)).
			SetPassword("migration-test-placeholder").SetFirstName("三").SetLastName("张").SaveX(ctx)
	}
	require.NoError(t, datamigrate.BackfillUserNames(ctx, client))
	for _, u := range client.User.Query().AllX(ctx) {
		require.Equal(t, "张三", u.Name)
	}
}
