package biz

import (
	"context"
	"fmt"
	"strings"

	"entgo.io/ent/dialect/sql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/intercept"
	"github.com/looplj/axonhub/internal/ent/request"
	"github.com/looplj/axonhub/internal/ent/requestexecution"
	"github.com/looplj/axonhub/internal/ent/usagelog"
	"github.com/looplj/axonhub/internal/objects"
)

type modelDisplayKey struct{}
type modelDisplayPolicy struct {
	Source        objects.BillingModelSource
	Admin         bool
	AdminProjects []int
}

func WithModelDisplay(ctx context.Context, source objects.BillingModelSource) context.Context {
	policy := modelDisplayPolicy{Source: source, Admin: authz.CanReadRequestDetails(ctx, 0)}
	if user, ok := contexts.GetUser(ctx); ok && user != nil && !policy.Admin {
		for _, membership := range user.Edges.ProjectUsers {
			if authz.CanReadRequestDetails(ctx, membership.ProjectID) {
				policy.AdminProjects = append(policy.AdminProjects, membership.ProjectID)
			}
		}
	}
	return context.WithValue(ctx, modelDisplayKey{}, policy)
}

func ModelDisplaySource(ctx context.Context) objects.BillingModelSource {
	p, _ := ctx.Value(modelDisplayKey{}).(modelDisplayPolicy)
	return p.Source
}

// ModelDisplayTable projects model_id before predicates, aggregation and pagination.
// Only trusted table/column names and integer project IDs enter the SQL fragment.
// Physical rows retain their routing IDs; background billing never uses this context.
func ModelDisplayTable(ctx context.Context, table string) string {
	p, ok := ctx.Value(modelDisplayKey{}).(modelDisplayPolicy)
	if !ok {
		return table
	}
	var columns []string
	var expr string
	original := "COALESCE(NULLIF(src.original_model_id, ''), '[original model unavailable]')"
	actual := "COALESCE((SELECT e.model_id FROM request_executions e WHERE e.request_id = src.id AND e.project_id = src.project_id ORDER BY e.created_at DESC, e.id DESC LIMIT 1), src.model_id)"
	switch table {
	case request.Table:
		columns = request.Columns
		expr = actual
		if p.Source == objects.BillingModelSourceOriginal {
			expr = original
		}
	case usagelog.Table:
		columns = usagelog.Columns
		expr = "src.model_id"
		if p.Source == objects.BillingModelSourceOriginal {
			expr = "COALESCE((SELECT NULLIF(r.original_model_id, '') FROM requests r WHERE r.id = src.request_id AND r.project_id = src.project_id), '[original model unavailable]')"
		}
	case requestexecution.Table:
		columns = requestexecution.Columns
		if p.Admin {
			return table
		}
		expr = "src.model_id"
		if p.Source == objects.BillingModelSourceOriginal {
			expr = "COALESCE((SELECT NULLIF(r.original_model_id, '') FROM requests r WHERE r.id = src.request_id AND r.project_id = src.project_id), '[original model unavailable]')"
			if len(p.AdminProjects) > 0 {
				ids := make([]string, len(p.AdminProjects))
				for i, id := range p.AdminProjects {
					ids[i] = fmt.Sprint(id)
				}
				expr = "CASE WHEN src.project_id IN (" + strings.Join(ids, ",") + ") THEN src.model_id ELSE " + expr + " END"
			}
		}
	default:
		return table
	}
	fields := make([]string, 0, len(columns))
	for _, column := range columns {
		if column == "model_id" {
			fields = append(fields, expr+" AS model_id")
		} else {
			fields = append(fields, "src."+column)
		}
	}
	return "(SELECT " + strings.Join(fields, ", ") + " FROM " + table + " src)"
}

// InstallModelDisplay applies equally to list queries, node lookups, nested
// connections and model filters. Tenant/privacy predicates remain on the outer query.
func InstallModelDisplay(client *ent.Client) {
	modifier := func(ctx context.Context, table string) func(*sql.Selector) {
		return func(s *sql.Selector) {
			if ModelDisplaySource(ctx) == "" {
				return
			}
			s.From(ModelDisplaySelector(ctx, table))
		}
	}
	client.Request.Intercept(intercept.TraverseRequest(func(ctx context.Context, q *ent.RequestQuery) error {
		if ModelDisplaySource(ctx) != "" {
			q.Where(modifier(ctx, request.Table))
		}
		return nil
	}))
	client.UsageLog.Intercept(intercept.TraverseUsageLog(func(ctx context.Context, q *ent.UsageLogQuery) error {
		if ModelDisplaySource(ctx) != "" {
			q.Where(modifier(ctx, usagelog.Table))
		}
		return nil
	}))
	client.RequestExecution.Intercept(intercept.TraverseRequestExecution(func(ctx context.Context, q *ent.RequestExecutionQuery) error {
		if ModelDisplaySource(ctx) != "" {
			q.Where(modifier(ctx, requestexecution.Table))
		}
		return nil
	}))
}

func ModelDisplaySelector(ctx context.Context, table string) *sql.Selector {
	projection := ModelDisplayTable(ctx, table)
	if projection == table {
		return sql.Select().From(sql.Table(table)).As(table)
	}
	fields := strings.TrimSuffix(strings.TrimPrefix(projection, "(SELECT "), " FROM "+table+" src)")
	fields = strings.ReplaceAll(fields, "src.", table+".")
	return sql.Select().SelectExpr(sql.Expr(fields)).From(sql.Table(table)).As(table)
}
