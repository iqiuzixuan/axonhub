package gql

import (
	"context"

	"github.com/99designs/gqlgen/graphql"
	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/privacy"
)

// Request content is protected at field resolution, including aliases, nodes,
// nested connections and cached/external bodies. Summary fields remain readable.
func requestDetailsMiddleware(ctx context.Context, next graphql.Resolver) (any, error) {
	field := graphql.GetFieldContext(ctx)
	if field == nil {
		return next(ctx)
	}
	protected := false
	preview := false
	switch field.Object {
	case "Request":
		switch field.Field.Name {
		case "requestHeaders", "requestBody", "responseBody", "responseChunks", "contentStorageKey":
			protected = true
		}
	case "RequestExecution":
		switch field.Field.Name {
		case "requestHeaders", "requestBody", "responseBody", "responseChunks", "errorMessage", "requestURL", "channelAPIKeyMasked":
			protected = true
		}
	case "Trace":
		switch field.Field.Name {
		case "rootSegment", "rawRootSegment", "firstText":
			protected = true
		case "firstUserQuery":
			protected, preview = true, true
		}
	case "Thread":
		if field.Field.Name == "firstUserQuery" {
			protected, preview = true, true
		}
	}
	if !protected {
		return next(ctx)
	}

	var object any
	if field.Parent != nil {
		object = field.Parent.Result
	}
	projectID, err := requestDetailsProjectID(ctx, object)
	if err == nil {
		err = authz.RequireRequestDetails(ctx, projectID)
	}
	if err != nil {
		// List previews are optional; omit them without failing the entire list.
		if preview {
			return nil, nil
		}
		return nil, err
	}
	return next(ctx)
}

func requestDetailsProjectID(ctx context.Context, object any) (int, error) {
	// Ent may select only the requested columns. If project_id is absent, resolve
	// it from the resource itself, with the existing query policy intact.
	switch obj := object.(type) {
	case *ent.Request:
		if obj.ProjectID != 0 {
			return obj.ProjectID, nil
		}
		resource, err := ent.FromContext(ctx).Request.Get(ctx, obj.ID)
		if err != nil {
			return 0, err
		}
		return resource.ProjectID, nil
	case *ent.RequestExecution:
		if obj.ProjectID != 0 {
			return obj.ProjectID, nil
		}
		request, err := obj.QueryRequest().Only(ctx)
		if err != nil {
			return 0, err
		}
		return request.ProjectID, nil
	case *ent.Trace:
		if obj.ProjectID != 0 {
			return obj.ProjectID, nil
		}
		resource, err := ent.FromContext(ctx).Trace.Get(ctx, obj.ID)
		if err != nil {
			return 0, err
		}
		return resource.ProjectID, nil
	case *ent.Thread:
		if obj.ProjectID != 0 {
			return obj.ProjectID, nil
		}
		resource, err := ent.FromContext(ctx).Thread.Get(ctx, obj.ID)
		if err != nil {
			return 0, err
		}
		return resource.ProjectID, nil
	default:
		return 0, privacy.Denyf("request details resource is unavailable")
	}
}
