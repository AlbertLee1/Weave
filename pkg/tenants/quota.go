// Package tenants implements per-tenant resource quotas for multi-tenant
// SaaS deployments (US-277). Tenant identity is sourced from the caller's
// auth.User.Attributes["realm"] — the same key the row-policy and
// feature-flag layers consume — so a single JWT claim governs all
// per-tenant limits without a separate sign-on flow.
//
// A Quota row carries three independent limits:
//
//	MaxObjects   total object count cap (storage rows). Enforced at write.
//	MaxStorage   total bytes-on-disk cap. Enforced at write.
//	MaxQPS       sustained request rate (req/sec). Enforced by middleware
//	             via a token bucket; bursts up to Burst (default 2x QPS).
//
// Zero values mean "no limit" so a freshly-inserted row is effectively
// unbounded until an operator dials it down.
package tenants

import (
	"errors"
	"fmt"
	"time"
)

// Quota is the per-tenant limit row. Tenant is the sole primary key —
// every other column is mutable via FlagUpdate-style PUT semantics.
type Quota struct {
	Tenant      string    `json:"tenant"`
	MaxObjects  int64     `json:"maxObjects"`
	MaxStorage  int64     `json:"maxStorage"`
	MaxQPS      float64   `json:"maxQPS"`
	Burst       int       `json:"burst"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// QuotaUpdate carries the optional-field PATCH semantics. nil pointer
// means "leave unchanged"; non-nil overwrites.
type QuotaUpdate struct {
	MaxObjects  *int64
	MaxStorage  *int64
	MaxQPS      *float64
	Burst       *int
	Description *string
}

// Errors surfaced to the admin API.
var (
	ErrQuotaNotFound      = errors.New("tenant quota not found")
	ErrQuotaAlreadyExists = errors.New("tenant quota already exists")
	ErrTenantInvalid      = errors.New("tenant identifier invalid")
)

// ValidateTenant enforces the same character set the SQL CHECK uses
// (alphanumerics + dot/underscore/hyphen, 1..128 chars). Mirrors
// pkg/featureflags.ValidateFlagName so admins see the same error at the
// API boundary they would see from PG.
func ValidateTenant(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty", ErrTenantInvalid)
	}
	if len(name) > 128 {
		return fmt.Errorf("%w: %d chars (max 128)", ErrTenantInvalid, len(name))
	}
	for _, r := range name {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return fmt.Errorf("%w: bad char %q", ErrTenantInvalid, r)
		}
	}
	return nil
}
