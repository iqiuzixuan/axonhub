package biz

import (
	"context"
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/enttest"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
)

func TestQuotaService_AllTime_RequestCountExceeded(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	apiKeyID := 1

	req1, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now.Add(-2 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req1.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetCreatedAt(now.Add(-2 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	req2, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now.Add(-1 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req2.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetCreatedAt(now.Add(-1 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)

	quota := &objects.APIKeyQuota{
		Requests: lo.ToPtr(int64(2)),
		Period: objects.APIKeyQuotaPeriod{
			Type: objects.APIKeyQuotaPeriodTypeAllTime,
		},
	}

	res, err := svc.CheckAPIKeyQuota(ctx, apiKeyID, quota)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Contains(t, res.Message, "requests quota exceeded")
}

func TestQuotaService_PastDuration_TotalTokensExceeded(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	apiKeyID := 2

	reqInWindow, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now.Add(-30 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(reqInWindow.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetSource(usagelog.SourceAPI).
		SetFormat("openai/chat_completions").
		SetPromptTokens(50).
		SetCompletionTokens(100).
		SetTotalTokens(150).
		SetTotalCost(1.0).
		SetCreatedAt(now.Add(-29 * time.Minute)).
		Save(ctx)
	require.NoError(t, err)

	reqOutWindow, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now.Add(-3 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(reqOutWindow.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetSource(usagelog.SourceAPI).
		SetFormat("openai/chat_completions").
		SetPromptTokens(10).
		SetCompletionTokens(10).
		SetTotalTokens(20).
		SetTotalCost(1.0).
		SetCreatedAt(now.Add(-3 * time.Hour)).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)
	quota := &objects.APIKeyQuota{
		TotalTokens: lo.ToPtr(int64(100)),
		Period: objects.APIKeyQuotaPeriod{
			Type: objects.APIKeyQuotaPeriodTypePastDuration,
			PastDuration: &objects.APIKeyQuotaPastDuration{
				Value: 1,
				Unit:  objects.APIKeyQuotaPastDurationUnitHour,
			},
		},
	}

	res, err := svc.CheckAPIKeyQuota(ctx, apiKeyID, quota)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Contains(t, res.Message, "total_tokens quota exceeded")
}

func TestQuotaService_PastDurationMinute_RequestCountExceeded(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	apiKeyID := 10

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now.Add(-10 * time.Second)).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetCreatedAt(now.Add(-10 * time.Second)).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)
	quota := &objects.APIKeyQuota{
		Requests: lo.ToPtr(int64(1)),
		Period: objects.APIKeyQuotaPeriod{
			Type: objects.APIKeyQuotaPeriodTypePastDuration,
			PastDuration: &objects.APIKeyQuotaPastDuration{
				Value: 1,
				Unit:  objects.APIKeyQuotaPastDurationUnitMinute,
			},
		},
	}

	res, err := svc.CheckAPIKeyQuota(ctx, apiKeyID, quota)
	require.NoError(t, err)
	require.False(t, res.Allowed)
	require.Contains(t, res.Message, "requests quota exceeded")
}

func TestQuotaService_PastDurationMinute_IncludesUsageAtWindowEnd(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	windowEnd := time.Now().UTC()
	apiKeyID := 11

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(windowEnd).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetTotalTokens(10).
		SetTotalCost(1.0).
		SetCreatedAt(windowEnd).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)
	window := QuotaWindow{
		Start:        lo.ToPtr(windowEnd.Add(-1 * time.Minute)),
		End:          lo.ToPtr(windowEnd),
		EndInclusive: true,
	}

	count, err := svc.requestCount(ctx, apiKeyID, window)
	require.NoError(t, err)
	require.Equal(t, int64(1), count)

	usage, err := svc.usageAgg(ctx, apiKeyID, window, true, true)
	require.NoError(t, err)
	require.Equal(t, int64(10), usage.TotalTokens)
	require.True(t, usage.TotalCost.Equal(decimal.NewFromFloat(1.0)))
}

func TestQuotaWindow_PastDurationMinute(t *testing.T) {
	now := time.Date(2026, 1, 20, 1, 2, 3, 0, time.UTC)
	window, err := quotaWindow(now, objects.APIKeyQuotaPeriod{
		Type: objects.APIKeyQuotaPeriodTypePastDuration,
		PastDuration: &objects.APIKeyQuotaPastDuration{
			Value: 5,
			Unit:  objects.APIKeyQuotaPastDurationUnitMinute,
		},
	}, time.UTC)
	require.NoError(t, err)
	require.NotNil(t, window.Start)
	require.NotNil(t, window.End)
	require.Equal(t, now.Add(-5*time.Minute), *window.Start)
	require.Equal(t, now, *window.End)
	require.True(t, window.EndInclusive)
}

func TestQuotaWindow_AllTime(t *testing.T) {
	now := time.Date(2026, 1, 20, 1, 2, 3, 0, time.UTC)
	window, err := quotaWindow(now, objects.APIKeyQuotaPeriod{
		Type: objects.APIKeyQuotaPeriodTypeAllTime,
	}, time.UTC)
	require.NoError(t, err)
	require.Nil(t, window.Start)
	require.NotNil(t, window.End)
	require.Equal(t, now, *window.End)
	require.True(t, window.EndInclusive)
}

func TestQuotaWindow_CalendarDay_Timezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	now := time.Date(2026, 1, 20, 1, 2, 3, 0, time.UTC)
	window, err := quotaWindow(now, objects.APIKeyQuotaPeriod{
		Type: objects.APIKeyQuotaPeriodTypeCalendarDuration,
		CalendarDuration: &objects.APIKeyQuotaCalendarDuration{
			Unit: objects.APIKeyQuotaCalendarDurationUnitDay,
		},
	}, loc)
	require.NoError(t, err)
	require.NotNil(t, window.Start)
	require.NotNil(t, window.End)

	require.Equal(t, time.Date(2026, 1, 19, 16, 0, 0, 0, time.UTC), *window.Start)
	require.Equal(t, time.Date(2026, 1, 20, 16, 0, 0, 0, time.UTC), *window.End)
	require.False(t, window.EndInclusive)
}

func TestQuotaWindow_CalendarMonth_Timezone(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	now := time.Date(2026, 1, 20, 1, 2, 3, 0, time.UTC)
	window, err := quotaWindow(now, objects.APIKeyQuotaPeriod{
		Type: objects.APIKeyQuotaPeriodTypeCalendarDuration,
		CalendarDuration: &objects.APIKeyQuotaCalendarDuration{
			Unit: objects.APIKeyQuotaCalendarDurationUnitMonth,
		},
	}, loc)
	require.NoError(t, err)
	require.NotNil(t, window.Start)
	require.NotNil(t, window.End)

	require.Equal(t, time.Date(2025, 12, 31, 16, 0, 0, 0, time.UTC), *window.Start)
	require.Equal(t, time.Date(2026, 1, 31, 16, 0, 0, 0, time.UTC), *window.End)
	require.False(t, window.EndInclusive)
}

func TestQuotaWindow_CalendarWeek(t *testing.T) {
	for _, tt := range []struct {
		name, timezone, now, start, end string
	}{
		{
			name: "midweek in system timezone", timezone: "Asia/Shanghai",
			now: "2026-09-11T07:00:00Z", start: "2026-09-06T16:00:00Z", end: "2026-09-13T16:00:00Z",
		},
		{
			name: "Sunday stays in current week", timezone: "Asia/Shanghai",
			now: "2026-09-13T15:59:59Z", start: "2026-09-06T16:00:00Z", end: "2026-09-13T16:00:00Z",
		},
		{
			name: "Monday midnight starts next week", timezone: "Asia/Shanghai",
			now: "2026-09-13T16:00:00Z", start: "2026-09-13T16:00:00Z", end: "2026-09-20T16:00:00Z",
		},
		{
			name: "UTC boundary", timezone: "UTC",
			now: "2026-09-13T16:00:00Z", start: "2026-09-07T00:00:00Z", end: "2026-09-14T00:00:00Z",
		},
		{
			name: "crosses month", timezone: "Asia/Shanghai",
			now: "2026-09-01T07:00:00Z", start: "2026-08-30T16:00:00Z", end: "2026-09-06T16:00:00Z",
		},
		{
			name: "crosses year", timezone: "Asia/Shanghai",
			now: "2026-01-01T07:00:00Z", start: "2025-12-28T16:00:00Z", end: "2026-01-04T16:00:00Z",
		},
		{
			name: "spring daylight saving makes a shorter week", timezone: "America/New_York",
			now: "2026-03-08T16:00:00Z", start: "2026-03-02T05:00:00Z", end: "2026-03-09T04:00:00Z",
		},
		{
			name: "autumn daylight saving makes a longer week", timezone: "America/New_York",
			now: "2026-11-01T16:00:00Z", start: "2026-10-26T04:00:00Z", end: "2026-11-02T05:00:00Z",
		},
		{
			name: "Sunday midnight skipped by daylight saving", timezone: "America/Sao_Paulo",
			now: "2018-11-04T15:00:00Z", start: "2018-10-29T03:00:00Z", end: "2018-11-05T02:00:00Z",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			loc, err := time.LoadLocation(tt.timezone)
			require.NoError(t, err)
			now, err := time.Parse(time.RFC3339, tt.now)
			require.NoError(t, err)
			window, err := quotaWindow(now, objects.APIKeyQuotaPeriod{
				Type: objects.APIKeyQuotaPeriodTypeCalendarDuration,
				CalendarDuration: &objects.APIKeyQuotaCalendarDuration{
					Unit: objects.APIKeyQuotaCalendarDurationUnitWeek,
				},
			}, loc)
			require.NoError(t, err)
			require.NotNil(t, window.Start)
			require.NotNil(t, window.End)
			require.Equal(t, tt.start, window.Start.Format(time.RFC3339))
			require.Equal(t, tt.end, window.End.Format(time.RFC3339))
			require.False(t, window.EndInclusive)
		})
	}
}

func TestQuotaService_CalendarDuration_ExcludesUsageAtWindowEnd(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	windowEnd := time.Date(2026, 1, 21, 0, 0, 0, 0, time.UTC)
	apiKeyID := 12

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(windowEnd).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetTotalTokens(10).
		SetTotalCost(1.0).
		SetCreatedAt(windowEnd).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)
	window := QuotaWindow{
		Start: lo.ToPtr(windowEnd.Add(-24 * time.Hour)),
		End:   lo.ToPtr(windowEnd),
	}

	count, err := svc.requestCount(ctx, apiKeyID, window)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)

	usage, err := svc.usageAgg(ctx, apiKeyID, window, true, true)
	require.NoError(t, err)
	require.Equal(t, int64(0), usage.TotalTokens)
	require.True(t, usage.TotalCost.Equal(decimal.Zero))
}

func TestQuotaService_CalendarDuration_CostExceeded(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()

	ctx := context.Background()
	ctx = ent.NewContext(ctx, client)
	ctx = authz.WithTestBypass(ctx)

	p, err := client.Project.Create().
		SetName("p").
		SetStatus(project.StatusActive).
		Save(ctx)
	require.NoError(t, err)

	now := time.Now().UTC()
	apiKeyID := 3

	req, err := client.Request.Create().
		SetProjectID(p.ID).
		SetAPIKeyID(apiKeyID).
		SetModelID("m").
		SetFormat("openai/chat_completions").
		SetStatus(request.StatusCompleted).
		SetRequestBody(objects.JSONRawMessage([]byte(`{}`))).
		SetCreatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	_, err = client.UsageLog.Create().
		SetRequestID(req.ID).
		SetAPIKeyID(apiKeyID).
		SetProjectID(p.ID).
		SetChannelID(1).
		SetModelID("m").
		SetSource(usagelog.SourceAPI).
		SetFormat("openai/chat_completions").
		SetPromptTokens(1).
		SetCompletionTokens(1).
		SetTotalTokens(2).
		SetTotalCost(11.0).
		SetCreatedAt(now).
		Save(ctx)
	require.NoError(t, err)

	systemService := NewSystemService(SystemServiceParams{Ent: client})
	svc := NewQuotaService(client, systemService)
	for _, unit := range []objects.APIKeyQuotaCalendarDurationUnit{
		objects.APIKeyQuotaCalendarDurationUnitDay,
		objects.APIKeyQuotaCalendarDurationUnitWeek,
		objects.APIKeyQuotaCalendarDurationUnitMonth,
	} {
		t.Run(string(unit), func(t *testing.T) {
			quota := &objects.APIKeyQuota{
				Cost: lo.ToPtr(decimal.NewFromFloat(10.0)),
				Period: objects.APIKeyQuotaPeriod{
					Type:             objects.APIKeyQuotaPeriodTypeCalendarDuration,
					CalendarDuration: &objects.APIKeyQuotaCalendarDuration{Unit: unit},
				},
			}

			res, err := svc.CheckAPIKeyQuota(ctx, apiKeyID, quota)
			require.NoError(t, err)
			require.False(t, res.Allowed)
			require.Contains(t, res.Message, "cost quota exceeded")

			usage, err := svc.GetQuota(ctx, apiKeyID, quota)
			require.NoError(t, err)
			require.Equal(t, res.Window, usage.Window)
			require.Equal(t, int64(1), usage.Usage.RequestCount)
			require.Equal(t, int64(2), usage.Usage.TotalTokens)
			require.True(t, usage.Usage.TotalCost.Equal(decimal.NewFromInt(11)))
		})
	}
}

func TestQuotaService_CalendarWeek_UsageBoundaries(t *testing.T) {
	client := enttest.NewEntClient(t, "sqlite3", "file:ent?mode=memory&_fk=0")
	defer client.Close()
	ctx := authz.WithTestBypass(ent.NewContext(t.Context(), client))
	p := client.Project.Create().SetName("weekly-quota").SaveX(ctx)
	apiKeyID := 12
	window, err := quotaWindow(time.Date(2026, 9, 11, 15, 0, 0, 0, time.UTC), objects.APIKeyQuotaPeriod{
		Type: objects.APIKeyQuotaPeriodTypeCalendarDuration,
		CalendarDuration: &objects.APIKeyQuotaCalendarDuration{
			Unit: objects.APIKeyQuotaCalendarDurationUnitWeek,
		},
	}, time.UTC)
	require.NoError(t, err)

	for i, at := range []time.Time{
		time.Date(2026, 9, 6, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 9, 13, 23, 59, 59, 0, time.UTC),
		time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC),
	} {
		client.UsageLog.Create().
			SetRequestID(i + 1).
			SetProjectID(p.ID).
			SetAPIKeyID(apiKeyID).
			SetModelID("test-model").
			SetTotalTokens(int64((i + 1) * 10)).
			SetTotalCost(float64(i + 1)).
			SetCreatedAt(at).
			SaveX(ctx)
	}

	svc := NewQuotaService(client, NewSystemService(SystemServiceParams{Ent: client}))
	count, err := svc.requestCount(ctx, apiKeyID, window)
	require.NoError(t, err)
	require.Equal(t, int64(2), count)
	usage, err := svc.usageAgg(ctx, apiKeyID, window, true, true)
	require.NoError(t, err)
	require.Equal(t, int64(50), usage.TotalTokens)
	require.True(t, usage.TotalCost.Equal(decimal.NewFromInt(5)))
}
