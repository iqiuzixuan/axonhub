package gql

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/99designs/gqlgen/graphql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/server/biz"
)

func applyModelDisplaySQL(ctx context.Context, query string) string {
	if biz.ModelDisplaySource(ctx) == "" {
		return query
	}
	query = strings.ReplaceAll(query, "JOIN requests r ON", "JOIN "+biz.ModelDisplayTable(ctx, "requests")+" r ON")
	// Display names must not reveal the developer/upstream identity of an alias.
	query = strings.ReplaceAll(query, "JOIN models m ON", "LEFT JOIN models m ON")
	query = strings.ReplaceAll(query, "m.name as model_name", "r.model_id as model_name")
	return query
}

// Relation predicates are generated as physical SQL subqueries rather than Ent
// traversals. Reject model probes through those predicates in private mode.
func hasNestedModelFilter(value reflect.Value, nested bool) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Struct:
		typ := value.Type()
		for i := 0; i < value.NumField(); i++ {
			field := value.Field(i)
			name := typ.Field(i).Name
			if nested && strings.HasPrefix(name, "ModelID") && !field.IsZero() {
				return true
			}
			if hasNestedModelFilter(field, nested || strings.HasPrefix(name, "Has")) {
				return true
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if hasNestedModelFilter(value.Index(i), nested) {
				return true
			}
		}
	}
	return false
}

func modelRoutingMiddleware(ctx context.Context, next graphql.Resolver) (any, error) {
	field := graphql.GetFieldContext(ctx)
	if field == nil || biz.ModelDisplaySource(ctx) != objects.BillingModelSourceOriginal {
		return next(ctx)
	}
	if authz.CanReadRequestDetails(ctx, 0) {
		return next(ctx)
	}
	if hasNestedModelFilter(reflect.ValueOf(field.Args["where"]), false) {
		return nil, fmt.Errorf("model filtering through internal execution relations is unavailable")
	}
	// Channel/model configuration is administrative data, not a public model catalogue.
	sensitive := false
	switch field.Object {
	case "Channel":
		sensitive = field.Field.Name == "supportedModels" || field.Field.Name == "defaultTestModel" || field.Field.Name == "settings"
	case "ModelSettings":
		if field.Field.Name == "associations" {
			return []*objects.ModelAssociation{}, nil
		}
	case "ChannelModelPrice", "ChannelModelPriceVersion":
		sensitive = field.Field.Name == "modelID"
	}
	if sensitive {
		return nil, fmt.Errorf("internal model configuration requires system administration")
	}
	return next(ctx)
}
