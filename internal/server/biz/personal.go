package biz

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/looplj/axonhub/internal/authz"
	"github.com/looplj/axonhub/internal/contexts"
	"github.com/looplj/axonhub/internal/ent"
	"github.com/looplj/axonhub/internal/ent/apikey"
	"github.com/looplj/axonhub/internal/ent/predicate"
	"github.com/looplj/axonhub/internal/ent/project"
	"github.com/looplj/axonhub/internal/ent/schema/schematype"
	"github.com/looplj/axonhub/internal/ent/user"
	"github.com/looplj/axonhub/internal/objects"
	"github.com/looplj/axonhub/internal/pkg/xerrors"
	"github.com/looplj/axonhub/internal/pkg/xtime"
)

// PersonalService serves self-service data without granting administrative scopes.
type PersonalService struct {
	*AbstractService
	models *ModelService
	system *SystemService
	quota  *QuotaService
}

func NewPersonalService(client *ent.Client, models *ModelService, system *SystemService, quota *QuotaService) *PersonalService {
	return &PersonalService{AbstractService: &AbstractService{db: client}, models: models, system: system, quota: quota}
}

// PersonalScope deliberately has no user ID: identity always comes from the session.
type PersonalScope struct {
	ProjectID *objects.GUID
	APIKeyID  *objects.GUID
}

type PersonalAPIKey struct {
	ID            objects.GUID
	Name          string
	ProjectID     objects.GUID
	ProjectName   string
	Status        apikey.Status
	Deleted       bool
	ActiveProfile string
}

type PersonalQuota struct {
	ProfileName string
	Limit       *objects.APIKeyQuota
	Start       *time.Time
	End         *time.Time
	Requests    float64
	Tokens      float64
	Cost        float64
}

type PersonalWorkspace struct {
	Timezone     string
	CurrencyCode string
	Today        string
	APIKeys      []*PersonalAPIKey
}

func (s *PersonalService) Workspace(ctx context.Context, projectID *objects.GUID) (*PersonalWorkspace, error) {
	keys, err := s.APIKeys(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return authz.RunWithSystemBypass(ctx, "personal-display-settings", func(readCtx context.Context) (*PersonalWorkspace, error) {
		settings, err := s.system.GeneralSettings(readCtx)
		if err != nil {
			return nil, err
		}
		loc := s.system.TimeLocation(readCtx)
		return &PersonalWorkspace{
			Timezone: loc.String(), CurrencyCode: settings.CurrencyCode,
			Today: xtime.UTCNow().In(loc).Format(time.DateOnly), APIKeys: keys,
		}, nil
	})
}

func requirePersonalUser(ctx context.Context) (*ent.User, error) {
	u, ok := contexts.GetUser(ctx)
	principal, hasPrincipal := authz.GetPrincipal(ctx)
	if !ok || u == nil || !hasPrincipal || !principal.IsUser() || principal.UserID == nil || *principal.UserID != u.ID {
		return nil, xerrors.UnauthorizedError("a signed-in user is required")
	}
	return u, nil
}

// personalKeys is the common ownership gate. It rechecks membership in the DB,
// includes deleted keys for historical statistics, and never uses read_api_keys
// (which also permits reading shared keys). No bypass context escapes this method.
func (s *PersonalService) personalKeys(ctx context.Context, scope PersonalScope) ([]*ent.APIKey, error) {
	u, err := requirePersonalUser(ctx)
	if err != nil {
		return nil, err
	}
	if scope.ProjectID != nil && (scope.ProjectID.Type != ent.TypeProject || scope.ProjectID.ID <= 0) {
		return nil, xerrors.ValidationError("invalid project ID")
	}
	if scope.APIKeyID != nil && (scope.APIKeyID.Type != ent.TypeAPIKey || scope.APIKeyID.ID <= 0) {
		return nil, xerrors.ValidationError("invalid API key ID")
	}
	return authz.RunWithSystemBypass(ctx, "personal-key-ownership", func(readCtx context.Context) ([]*ent.APIKey, error) {
		client := s.entFromContext(ctx)
		// Authentication can outlive a membership or account status change.
		currentUser, err := client.User.Query().Where(user.IDEQ(u.ID), user.StatusEQ(user.StatusActivated)).Only(readCtx)
		if ent.IsNotFound(err) {
			return nil, xerrors.ForbiddenError("user is not active")
		}
		if err != nil {
			return nil, err
		}
		// SkipSoftDelete below is only intended to retain key history. Explicitly
		// exclude deleted projects even when querying their keys with that context.
		projects := []predicate.Project{project.StatusEQ(project.StatusActive), project.DeletedAtEQ(0)}
		if !currentUser.IsOwner {
			projects = append(projects, project.HasUsersWith(user.IDEQ(u.ID)))
		}
		if scope.ProjectID != nil {
			projects = append(projects, project.IDEQ(scope.ProjectID.ID))
			allowed, err := client.Project.Query().Where(projects...).Exist(readCtx)
			if err != nil {
				return nil, err
			}
			if !allowed {
				return nil, xerrors.ForbiddenError("project is not accessible")
			}
		}
		query := client.APIKey.Query().
			Where(apikey.UserIDEQ(u.ID), apikey.TypeEQ(apikey.TypePersonal), apikey.HasProjectWith(projects...)).
			WithProject().Order(ent.Asc(apikey.FieldID))
		if scope.APIKeyID != nil {
			query.Where(apikey.IDEQ(scope.APIKeyID.ID))
		}
		keys, err := query.All(schematype.SkipSoftDelete(readCtx))
		if err != nil {
			return nil, fmt.Errorf("load personal API keys: %w", err)
		}
		if scope.APIKeyID != nil && len(keys) == 0 {
			return nil, xerrors.ForbiddenError("API key is not accessible")
		}
		return keys, nil
	})
}

func personalKeyInfo(key *ent.APIKey) *PersonalAPIKey {
	profileName := ""
	if profile := key.GetActiveProfile(); profile != nil {
		profileName = profile.Name
	}
	return &PersonalAPIKey{
		ID: objects.GUID{Type: ent.TypeAPIKey, ID: key.ID}, Name: key.Name,
		ProjectID:   objects.GUID{Type: ent.TypeProject, ID: key.ProjectID},
		ProjectName: key.Edges.Project.Name, Status: key.Status,
		Deleted: key.DeletedAt != 0, ActiveProfile: profileName,
	}
}

func (s *PersonalService) APIKeys(ctx context.Context, projectID *objects.GUID) ([]*PersonalAPIKey, error) {
	keys, err := s.personalKeys(ctx, PersonalScope{ProjectID: projectID})
	if err != nil {
		return nil, err
	}
	result := make([]*PersonalAPIKey, 0, len(keys))
	for _, key := range keys {
		result = append(result, personalKeyInfo(key))
	}
	return result, nil
}

func personalKeyCanCall(key *ent.APIKey) bool {
	return key.DeletedAt == 0 && key.Status == apikey.StatusEnabled &&
		slices.Contains(key.Scopes, "read_channels") && slices.Contains(key.Scopes, "write_requests")
}

func (s *PersonalService) activeQuota(ctx context.Context, key *ent.APIKey) (*PersonalQuota, error) {
	profile := key.GetActiveProfile()
	if profile == nil || profile.Quota == nil || key.DeletedAt != 0 {
		return nil, nil
	}
	result, err := s.quota.GetQuota(ctx, key.ID, profile.Quota)
	if err != nil {
		return nil, err
	}
	cost, _ := result.Usage.TotalCost.Float64()
	return &PersonalQuota{
		ProfileName: profile.Name, Limit: profile.Quota, Start: result.Window.Start, End: result.Window.End,
		Requests: float64(result.Usage.RequestCount), Tokens: float64(result.Usage.TotalTokens), Cost: cost,
	}, nil
}
