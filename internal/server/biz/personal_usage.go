package biz

import (
	"context"
	"fmt"
	"sort"
	"time"

	"entgo.io/ent/dialect/sql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xerrors"
)

type PersonalUsageInput struct {
	ProjectID *objects.GUID
	APIKeyID  *objects.GUID
	StartDate string
	EndDate   string
}

// Counters use Float in GraphQL so token totals do not overflow GraphQL's
// signed 32-bit Int. Integers remain exact up to JavaScript's safe integer limit.
type PersonalUsageMetrics struct {
	Requests           float64
	SuccessfulRequests float64
	FailedRequests     float64
	CanceledRequests   float64
	PendingRequests    float64
	InputTokens        float64
	OutputTokens       float64
	CachedTokens       float64
	TotalTokens        float64
	Cost               float64
	UnpricedUsageCount float64
	SuccessRate        *float64
}

type PersonalDailyUsage struct {
	Date    string
	Metrics *PersonalUsageMetrics
}

type PersonalModelUsage struct {
	ModelID string
	Metrics *PersonalUsageMetrics
}

type PersonalAPIKeyUsage struct {
	APIKey  *PersonalAPIKey
	Metrics *PersonalUsageMetrics
	Quota   *PersonalQuota
}

type PersonalDashboard struct {
	Timezone string
	Overview *PersonalUsageMetrics
	Daily    []*PersonalDailyUsage
	Models   []*PersonalModelUsage
	APIKeys  []*PersonalAPIKeyUsage
}

func personalDateRange(startDate, endDate string, loc *time.Location) ([]time.Time, error) {
	start, err := time.ParseInLocation(time.DateOnly, startDate, loc)
	if err != nil {
		return nil, xerrors.ValidationError("invalid start date: expected YYYY-MM-DD")
	}
	end, err := time.ParseInLocation(time.DateOnly, endDate, loc)
	if err != nil || end.Before(start) {
		return nil, xerrors.ValidationError("invalid end date")
	}
	days := make([]time.Time, 0, 31)
	for day := start; !day.After(end); day = day.AddDate(0, 0, 1) {
		if len(days) == 93 {
			return nil, xerrors.ValidationError("date range cannot exceed 93 days")
		}
		days = append(days, day)
	}
	return days, nil
}

// A bounded CASE uses real UTC boundaries for each local calendar day.
// Unlike a fixed UTC offset, this works across DST and half/quarter-hour zones
// on every supported database, without requiring timezone tables in MySQL.
func personalDayExpression(column string, days []time.Time) sql.Querier {
	return sql.ExprFunc(func(b *sql.Builder) {
		b.WriteString("CASE")
		for _, day := range days {
			b.WriteString(" WHEN ").WriteString(column).WriteString(" < ").Arg(day.AddDate(0, 0, 1).UTC()).
				WriteString(" THEN ").Arg(day.Format(time.DateOnly))
		}
		b.WriteString(" END")
	})
}

type personalRequestRow struct {
	Day     string         `json:"day"`
	KeyID   int            `json:"key_id"`
	ModelID string         `json:"model_id"`
	Status  request.Status `json:"status"`
	Count   float64        `json:"count"`
}

type personalTokenRow struct {
	Day      string  `json:"day"`
	KeyID    int     `json:"key_id"`
	ModelID  string  `json:"model_id"`
	Input    float64 `json:"input_tokens"`
	Output   float64 `json:"output_tokens"`
	Cached   float64 `json:"cached_tokens"`
	Cost     float64 `json:"cost"`
	Unpriced float64 `json:"unpriced"`
}

func (s *PersonalService) Dashboard(ctx context.Context, input PersonalUsageInput) (*PersonalDashboard, error) {
	keys, err := s.personalKeys(ctx, PersonalScope{ProjectID: input.ProjectID, APIKeyID: input.APIKeyID})
	if err != nil {
		return nil, err
	}
	loc, err := authz.RunWithSystemBypass(ctx, "personal-usage-timezone", func(readCtx context.Context) (*time.Location, error) {
		return s.system.TimeLocation(readCtx), nil
	})
	if err != nil {
		return nil, err
	}
	days, err := personalDateRange(input.StartDate, input.EndDate, loc)
	if err != nil {
		return nil, err
	}
	result := &PersonalDashboard{
		Timezone: loc.String(), Overview: &PersonalUsageMetrics{},
		Daily: []*PersonalDailyUsage{}, Models: []*PersonalModelUsage{}, APIKeys: []*PersonalAPIKeyUsage{},
	}
	daily := make(map[string]*PersonalUsageMetrics, len(days))
	for _, day := range days {
		date := day.Format(time.DateOnly)
		daily[date] = &PersonalUsageMetrics{}
		result.Daily = append(result.Daily, &PersonalDailyUsage{Date: date, Metrics: daily[date]})
	}
	byKey := make(map[int]*PersonalUsageMetrics, len(keys))
	byModel := make(map[string]*PersonalUsageMetrics)
	keyIDs, projectIDs := make([]int, 0, len(keys)), make([]int, 0, len(keys))
	projectSeen := make(map[int]bool)
	for _, key := range keys {
		keyIDs = append(keyIDs, key.ID)
		if !projectSeen[key.ProjectID] {
			projectIDs = append(projectIDs, key.ProjectID)
			projectSeen[key.ProjectID] = true
		}
		quota, err := s.activeQuota(ctx, key)
		if err != nil {
			return nil, fmt.Errorf("read personal quota: %w", err)
		}
		byKey[key.ID] = &PersonalUsageMetrics{}
		result.APIKeys = append(result.APIKeys, &PersonalAPIKeyUsage{APIKey: personalKeyInfo(key), Metrics: byKey[key.ID], Quota: quota})
	}
	if len(keyIDs) == 0 {
		return result, nil // Never turn an empty ownership set into an unfiltered query.
	}
	start, end := days[0].UTC(), days[len(days)-1].AddDate(0, 0, 1).UTC()
	var requests []personalRequestRow
	var tokens []personalTokenRow
	err = authz.RunWithSystemBypassVoid(ctx, "personal-usage-aggregation", func(readCtx context.Context) error {
		client := s.entFromContext(ctx)
		err := client.Request.Query().
			Where(request.APIKeyIDIn(keyIDs...), request.ProjectIDIn(projectIDs...), request.CreatedAtGTE(start), request.CreatedAtLT(end)).
			Modify(func(q *sql.Selector) {
				q.Select(
					sql.As(q.C(request.FieldAPIKeyID), "key_id"),
					q.C(request.FieldModelID), q.C(request.FieldStatus),
					sql.As(sql.Count(q.C(request.FieldID)), "count"),
				).AppendSelectExprAs(personalDayExpression(q.C(request.FieldCreatedAt), days), "day").
					GroupBy(q.C(request.FieldAPIKeyID), q.C(request.FieldModelID), q.C(request.FieldStatus), "day")
			}).Scan(readCtx, &requests)
		if err != nil {
			return err
		}
		return client.UsageLog.Query().
			Where(usagelog.APIKeyIDIn(keyIDs...), usagelog.ProjectIDIn(projectIDs...), usagelog.CreatedAtGTE(start), usagelog.CreatedAtLT(end)).
			Modify(func(q *sql.Selector) {
				r := sql.Table(request.Table)
				q.Join(r).On(q.C(usagelog.FieldRequestID), r.C(request.FieldID))
				q.Where(sql.ColumnsEQ(q.C(usagelog.FieldAPIKeyID), r.C(request.FieldAPIKeyID)))
				q.Where(sql.ColumnsEQ(q.C(usagelog.FieldProjectID), r.C(request.FieldProjectID)))
				sum := func(field, alias string) string {
					return sql.As(fmt.Sprintf("COALESCE(SUM(%s), 0)", q.C(field)), alias)
				}
				q.Select(
					sql.As(q.C(usagelog.FieldAPIKeyID), "key_id"),
					sql.As(r.C(request.FieldModelID), "model_id"),
					sum(usagelog.FieldPromptTokens, "input_tokens"),
					sum(usagelog.FieldCompletionTokens, "output_tokens"),
					sum(usagelog.FieldPromptCachedTokens, "cached_tokens"),
					sum(usagelog.FieldTotalCost, "cost"),
					sql.As(fmt.Sprintf("SUM(CASE WHEN %s IS NULL THEN 1 ELSE 0 END)", q.C(usagelog.FieldTotalCost)), "unpriced"),
				).AppendSelectExprAs(personalDayExpression(q.C(usagelog.FieldCreatedAt), days), "day").
					GroupBy(q.C(usagelog.FieldAPIKeyID), r.C(request.FieldModelID), "day")
			}).Scan(readCtx, &tokens)
	})
	if err != nil {
		return nil, fmt.Errorf("aggregate personal usage: %w", err)
	}
	targets := func(day string, keyID int, modelID string) []*PersonalUsageMetrics {
		if byModel[modelID] == nil {
			byModel[modelID] = &PersonalUsageMetrics{}
		}
		return []*PersonalUsageMetrics{result.Overview, daily[day], byKey[keyID], byModel[modelID]}
	}
	for _, row := range requests {
		for _, metric := range targets(row.Day, row.KeyID, row.ModelID) {
			metric.Requests += row.Count
			switch row.Status {
			case request.StatusCompleted:
				metric.SuccessfulRequests += row.Count
			case request.StatusFailed:
				metric.FailedRequests += row.Count
			case request.StatusCanceled:
				metric.CanceledRequests += row.Count
			default:
				metric.PendingRequests += row.Count
			}
		}
	}
	for _, row := range tokens {
		for _, metric := range targets(row.Day, row.KeyID, row.ModelID) {
			metric.InputTokens += row.Input
			metric.OutputTokens += row.Output
			metric.CachedTokens += row.Cached
			// Cached/reasoning detail tokens are already included in input/output.
			metric.TotalTokens += row.Input + row.Output
			metric.Cost += row.Cost
			metric.UnpricedUsageCount += row.Unpriced
		}
	}
	finalize := func(metric *PersonalUsageMetrics) {
		if finished := metric.SuccessfulRequests + metric.FailedRequests; finished > 0 {
			rate := 100 * metric.SuccessfulRequests / finished
			metric.SuccessRate = &rate
		}
	}
	finalize(result.Overview)
	for _, item := range result.Daily {
		finalize(item.Metrics)
	}
	for _, item := range result.APIKeys {
		finalize(item.Metrics)
	}
	for modelID, metric := range byModel {
		finalize(metric)
		result.Models = append(result.Models, &PersonalModelUsage{ModelID: modelID, Metrics: metric})
	}
	sort.Slice(result.Models, func(i, j int) bool {
		if result.Models[i].Metrics.TotalTokens == result.Models[j].Metrics.TotalTokens {
			return result.Models[i].ModelID < result.Models[j].ModelID
		}
		return result.Models[i].Metrics.TotalTokens > result.Models[j].Metrics.TotalTokens
	})
	return result, nil
}
