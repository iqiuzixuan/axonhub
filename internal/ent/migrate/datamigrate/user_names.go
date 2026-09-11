package datamigrate

import (
	"context"
	"fmt"
	"strings"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/pkg/xname"
)

// BackfillUserNames is deliberately independent of upstream release versions:
// a fork can adopt this field from any upstream version. Empty names identify
// legacy rows; existing names and the legacy source columns remain unchanged.
// Call after Ent schema migration has added the name column.
func BackfillUserNames(ctx context.Context, client *ent.Client) (err error) {
	ctx = schematype.SkipSoftDelete(authz.WithSystemBypass(ctx, "backfill-user-names"))
	ctx, tx, err := client.OpenTx(ctx)
	if err != nil {
		return fmt.Errorf("start user name migration: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	txClient := ent.FromContext(ctx)

	lastID := 0
	for {
		users, queryErr := txClient.User.Query().Where(user.NameEQ(""), user.IDGT(lastID)).
			Order(ent.Asc(user.FieldID)).Limit(200).All(ctx)
		if queryErr != nil {
			return fmt.Errorf("read legacy user names: %w", queryErr)
		}
		if len(users) == 0 {
			break
		}
		for _, u := range users {
			name := xname.FromLegacy(u.FirstName, u.LastName)
			if name == "" {
				name = strings.TrimSpace(u.Email)
			}
			if name == "" {
				name = "User"
			}
			// Do not apply new input length limits to historical data: preserve it.
			if _, updateErr := txClient.User.Update().Where(user.IDEQ(u.ID), user.NameEQ("")).SetName(name).Save(ctx); updateErr != nil {
				return fmt.Errorf("backfill user %d name: %w", u.ID, updateErr)
			}
			lastID = u.ID
		}
	}
	return tx.Commit()
}
