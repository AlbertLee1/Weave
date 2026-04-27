package main

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/liyang/weave/pkg/tenants"
)

// isTenantQuotaUniqueViolation reports whether err is a PG unique-index
// violation (SQLSTATE 23505). Inline rather than depending on pgconn so
// the cmd/server binary stays free of an extra driver path. Mirrors the
// shape of isFeatureFlagUniqueViolation (US-276).
func isTenantQuotaUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLSTATE 23505") ||
		strings.Contains(msg, "duplicate key value")
}

// pgTenantQuotaStore satisfies tenants.Store by persisting tenant_quota
// rows to the tenant_quotas table (US-277). Lives in cmd/server/ so
// pkg/tenants stays free of pgx — same dep-direction trick used by
// pgFeatureFlagsStore (US-276) and pgGDPRJobStore.
type pgTenantQuotaStore struct {
	pool *pgxpool.Pool
}

func newPGTenantQuotaStore(pool *pgxpool.Pool) *pgTenantQuotaStore {
	return &pgTenantQuotaStore{pool: pool}
}

func (s *pgTenantQuotaStore) CreateQuota(ctx context.Context, q *tenants.Quota) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO tenant_quotas (tenant, max_objects, max_storage, max_qps, burst, description)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		q.Tenant, q.MaxObjects, q.MaxStorage, q.MaxQPS, q.Burst, q.Description,
	)
	if err != nil {
		if isTenantQuotaUniqueViolation(err) {
			return tenants.ErrQuotaAlreadyExists
		}
		return err
	}
	fresh, err := s.GetQuota(ctx, q.Tenant)
	if err != nil {
		return err
	}
	*q = *fresh
	return nil
}

func (s *pgTenantQuotaStore) GetQuota(ctx context.Context, tenant string) (*tenants.Quota, error) {
	var q tenants.Quota
	err := s.pool.QueryRow(ctx,
		`SELECT tenant, max_objects, max_storage, max_qps, burst, description, created_at, updated_at
		 FROM tenant_quotas WHERE tenant = $1`, tenant).
		Scan(&q.Tenant, &q.MaxObjects, &q.MaxStorage, &q.MaxQPS, &q.Burst,
			&q.Description, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, tenants.ErrQuotaNotFound
		}
		return nil, err
	}
	return &q, nil
}

func (s *pgTenantQuotaStore) ListQuotas(ctx context.Context) ([]*tenants.Quota, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT tenant, max_objects, max_storage, max_qps, burst, description, created_at, updated_at
		 FROM tenant_quotas ORDER BY tenant ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*tenants.Quota
	for rows.Next() {
		var q tenants.Quota
		if err := rows.Scan(&q.Tenant, &q.MaxObjects, &q.MaxStorage, &q.MaxQPS,
			&q.Burst, &q.Description, &q.CreatedAt, &q.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, &q)
	}
	return out, rows.Err()
}

func (s *pgTenantQuotaStore) UpdateQuota(ctx context.Context, tenant string, upd tenants.QuotaUpdate) error {
	args := []interface{}{}
	sets := []string{"updated_at = NOW()"}
	argN := 1
	if upd.MaxObjects != nil {
		sets = append(sets, "max_objects = $"+strconv.Itoa(argN))
		args = append(args, *upd.MaxObjects)
		argN++
	}
	if upd.MaxStorage != nil {
		sets = append(sets, "max_storage = $"+strconv.Itoa(argN))
		args = append(args, *upd.MaxStorage)
		argN++
	}
	if upd.MaxQPS != nil {
		sets = append(sets, "max_qps = $"+strconv.Itoa(argN))
		args = append(args, *upd.MaxQPS)
		argN++
	}
	if upd.Burst != nil {
		sets = append(sets, "burst = $"+strconv.Itoa(argN))
		args = append(args, *upd.Burst)
		argN++
	}
	if upd.Description != nil {
		sets = append(sets, "description = $"+strconv.Itoa(argN))
		args = append(args, *upd.Description)
		argN++
	}
	args = append(args, tenant)
	tag, err := s.pool.Exec(ctx,
		`UPDATE tenant_quotas SET `+strings.Join(sets, ", ")+
			` WHERE tenant = $`+strconv.Itoa(argN),
		args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tenants.ErrQuotaNotFound
	}
	return nil
}

func (s *pgTenantQuotaStore) DeleteQuota(ctx context.Context, tenant string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM tenant_quotas WHERE tenant = $1`, tenant)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return tenants.ErrQuotaNotFound
	}
	return nil
}
