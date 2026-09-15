package biz

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/log"
	"github.com/looplj/axonhub/internal/server/scheduler"
	"github.com/tidwall/gjson"
)

// originalModelFromBody accepts only the top-level model in the client payload.
// Routing IDs, execution payloads and responses are never recovery sources.
func originalModelFromBody(body []byte) string {
	if !gjson.ValidBytes(body) {
		return ""
	}
	model := gjson.GetBytes(body, "model")
	if model.Type != gjson.String || strings.TrimSpace(model.Str) == "" {
		return ""
	}
	return model.Str
}

// Recovery runs outside request/list queries, reads one payload at a time and
// prioritizes recent history. Successful rows are excluded on every later pass.
// Retry missing/unreadable payloads hourly, since external storage can recover.
func (s *RequestService) registerOriginalModelRecovery(ctx context.Context, sched *scheduler.Scheduler) error {
	var mu sync.Mutex
	var beforeID int
	var nextPass time.Time
	return sched.Register(ctx, scheduler.TaskSpec{
		Name:        "request-original-model-recovery",
		Description: "Recover original model IDs from retained client request bodies",
		FixRate:     10 * time.Second,
	}, func(ctx context.Context) {
		if !mu.TryLock() {
			return
		}
		defer mu.Unlock()
		if time.Now().Before(nextPass) {
			return
		}
		ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
		defer cancel()
		next, recovered, err := s.recoverOriginalModelsBatch(ctx, beforeID, 100)
		beforeID = next
		if err != nil {
			log.Warn(ctx, "Original model recovery will resume", log.Cause(err))
			return
		}
		if recovered > 0 {
			log.Info(ctx, "Recovered original request models", log.Int("count", recovered))
		}
		if beforeID == 0 {
			nextPass = time.Now().Add(time.Hour)
		}
	})
}

// The cursor advances only after a row is processed. A timeout or DB failure
// resumes at the failed row. Conditional writes also make multiple replicas safe.
func (s *RequestService) recoverOriginalModelsBatch(ctx context.Context, beforeID, limit int) (nextID, recovered int, err error) {
	ctx = authz.WithSystemBypass(ctx, "recover-original-request-models")
	client := s.entFromContext(ctx)
	ctx = ent.NewContext(ctx, client)
	nextID = beforeID
	query := client.Request.Query().Where(
		request.Or(request.OriginalModelIDIsNil(), request.OriginalModelIDEQ("")),
		request.StatusNEQ(request.StatusProcessing),
	).Select(request.FieldID, request.FieldProjectID, request.FieldDataStorageID).
		Order(ent.Desc(request.FieldID)).Limit(limit)
	if beforeID > 0 {
		query.Where(request.IDLT(beforeID))
	}
	rows, err := query.All(ctx)
	if err != nil {
		return nextID, recovered, fmt.Errorf("find legacy request models: %w", err)
	}
	if len(rows) == 0 {
		return 0, recovered, nil
	}
	for _, row := range rows {
		// Do not select response bodies, streams or a whole batch of large JSON.
		payload, loadErr := client.Request.Query().Where(request.ID(row.ID)).
			Select(request.FieldRequestBody).Only(ctx)
		if ent.IsNotFound(loadErr) {
			nextID = row.ID
			continue
		}
		if loadErr != nil {
			return nextID, recovered, fmt.Errorf("read client request %d: %w", row.ID, loadErr)
		}
		row.RequestBody = payload.RequestBody
		model := originalModelFromBody(row.RequestBody)
		if model == "" && row.DataStorageID != 0 {
			body, loadErr := s.LoadRequestBody(ctx, row)
			if loadErr != nil {
				return nextID, recovered, loadErr
			}
			model = originalModelFromBody(body)
		}
		if err := ctx.Err(); err != nil {
			return nextID, recovered, err
		}
		if model != "" {
			// This immutable field is repaired explicitly; do not touch timestamps,
			// routing, billing snapshots, usage logs or historical charges.
			builder := sql.Dialect(client.Driver().Dialect()).Update(request.Table).
				Set(request.FieldOriginalModelID, model).
				Where(sql.And(sql.EQ(request.FieldID, row.ID), sql.Or(
					sql.IsNull(request.FieldOriginalModelID), sql.EQ(request.FieldOriginalModelID, ""))))
			statement, args := builder.Query()
			var result sql.Result
			if err := client.Driver().Exec(ctx, statement, args, &result); err != nil {
				return nextID, recovered, fmt.Errorf("recover client model %d: %w", row.ID, err)
			}
			count, err := result.RowsAffected()
			if err != nil {
				return nextID, recovered, err
			}
			recovered += int(count)
		}
		nextID = row.ID
	}
	return nextID, recovered, nil
}
