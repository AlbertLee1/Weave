package main

import (
	"context"
	"io"

	"github.com/liyang/weave/pkg/audit"
	"github.com/liyang/weave/pkg/auth"
	"github.com/liyang/weave/pkg/gdpr"
	"github.com/liyang/weave/pkg/media"
	"github.com/liyang/weave/pkg/oms"
)

// buildGDPRExporter composes a gdpr.Exporter from whichever subsystems
// are wired on deps. Each source is optional — nil subsystems produce
// nil sources and the exporter omits those fields from the bundle. The
// return is non-nil as long as any source could be wired; callers check
// `nil` to decide whether to mount the /export route.
func buildGDPRExporter(
	userRepo auth.UserRepository,
	auditStore audit.Store,
	mediaCatalog oms.MediaAssetStore,
	mediaStore *media.Store,
) *gdpr.Exporter {
	e := gdpr.NewExporter()
	if userRepo != nil {
		e.Profile = &userProfileSource{repo: userRepo}
		e.Roles = &userRoleSource{repo: userRepo}
		e.OntRoles = &userOntRoleSource{repo: userRepo}
	}
	if auditStore != nil {
		e.Audit = &auditActorSource{store: auditStore}
	}
	if mediaCatalog != nil {
		e.MediaAssets = &mediaCatalogSource{catalog: mediaCatalog}
	}
	if mediaStore != nil {
		e.MediaBlobs = &mediaBlobAdapter{store: mediaStore}
	}
	return e
}

// userProfileSource maps auth.UserRepository.GetUserByID into a
// gdpr.ExportProfile. Sensitive fields (password hash, MFA secret) are
// intentionally NOT copied.
type userProfileSource struct{ repo auth.UserRepository }

func (s *userProfileSource) Profile(ctx context.Context, userID string) (*gdpr.ExportProfile, error) {
	u, err := s.repo.GetUserByID(ctx, userID)
	if err != nil {
		if err == auth.ErrUserNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &gdpr.ExportProfile{
		ID:         u.ID,
		Email:      u.Email,
		Name:       u.Name,
		MFAEnabled: u.MFAEnabled,
		CreatedAt:  u.CreatedAt,
		UpdatedAt:  u.UpdatedAt,
	}, nil
}

// userRoleSource adapts ListUserRoles.
type userRoleSource struct{ repo auth.UserRepository }

func (s *userRoleSource) ListUserRoles(ctx context.Context, userID string) ([]string, error) {
	return s.repo.ListUserRoles(ctx, userID)
}

// userOntRoleSource adapts ListUserOntologyRoles.
type userOntRoleSource struct{ repo auth.UserRepository }

func (s *userOntRoleSource) ListUserOntologyRoles(ctx context.Context, userID string) (map[string]string, error) {
	return s.repo.ListUserOntologyRoles(ctx, userID)
}

// auditActorSource wraps audit.Store.List({ActorID: userID}). The inner
// store is expected to be the already-decorated RedactingStore+TeeStore
// chain so the export inherits whatever retention/redaction rules apply
// to the public audit API.
type auditActorSource struct{ store audit.Store }

func (s *auditActorSource) ListByActor(ctx context.Context, userID string) ([]audit.AuditEvent, error) {
	return s.store.List(ctx, audit.ListFilter{ActorID: userID})
}

// mediaCatalogSource maps oms.MediaAssetStore.ListByCreatedBy into the
// gdpr-scoped MediaAssetInfo shape. The limit is hard-coded to 10_000 —
// an individual user with more than ten thousand uploaded media assets is
// outside the v1 scope of data portability; a future revision can stream
// in pages if the limit is hit.
type mediaCatalogSource struct{ catalog oms.MediaAssetStore }

func (s *mediaCatalogSource) ListUserMedia(ctx context.Context, userID string) ([]gdpr.MediaAssetInfo, error) {
	assets, err := s.catalog.ListByCreatedBy(ctx, userID, 10_000)
	if err != nil {
		return nil, err
	}
	out := make([]gdpr.MediaAssetInfo, 0, len(assets))
	for _, a := range assets {
		out = append(out, gdpr.MediaAssetInfo{
			RID:       a.RID,
			Realm:     a.Realm,
			Filename:  a.Filename,
			MIME:      a.MIME,
			SizeBytes: a.SizeBytes,
			SHA256:    a.SHA256,
			Path:      a.Path,
			CreatedAt: a.CreatedAt,
		})
	}
	return out, nil
}

// mediaBlobAdapter bridges *media.Store.Open to gdpr.MediaBlobSource.
type mediaBlobAdapter struct{ store *media.Store }

func (a *mediaBlobAdapter) Open(ctx context.Context, relPath string) (io.ReadCloser, error) {
	return a.store.Open(ctx, relPath)
}
