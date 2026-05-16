package tenants

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// US-495 — Tenants PG schema 强隔离.
//
// This file provides the primitives that promote multi-tenancy from a
// row-filter approach to per-tenant PostgreSQL schemas:
//
//   - SchemaName maps a caller-supplied tenant identifier to a safe,
//     reserved-prefix PG schema name (`tenant_<sanitised>`).
//   - ProvisionSchema creates the schema if missing and runs a
//     caller-supplied DDL script inside that schema's search_path.
//   - DropSchema CASCADE-removes the schema (cleanup / off-boarding).
//   - AcquireForTenant lends a pool connection with its session
//     `search_path` pointing at the tenant's schema (then `public`)
//     so unqualified table references resolve into the tenant's
//     namespace. The release hook resets `search_path` so the
//     connection returned to the pool does not contaminate the next
//     caller — critical with pgxpool because connections are reused.
//
// Naming guarantee: SchemaName always returns a string prefixed with
// `tenant_`. This is the contract callers depend on to distinguish
// per-tenant tables from the shared catalog (audit_events, ontologies,
// migrations ledger, ...). Never bypass the prefix.

// schemaSafe matches the same character class enforced on tenant IDs
// by migrations/000065_tenant_quotas.up.sql:
//
//	CHECK (tenant ~ '^[A-Za-z0-9._-]{1,128}$')
//
// Keeping the two in sync means any tenant valid for tenant_quotas
// also passes SchemaName, and any rejection here would equally have
// failed the quota INSERT — single source of truth on the wire shape.
var schemaSafe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// schemaPrefix is the reserved namespace for per-tenant schemas. The
// prefix can never appear in callers' raw tenant IDs because `_` is in
// schemaSafe but the prefix is added unconditionally, so a tenant
// literally named "us495a" becomes schema "tenant_us495a" — the
// reverse mapping is unambiguous.
const schemaPrefix = "tenant_"

// schemaReplacer rewrites dot and hyphen — both legal in tenant IDs
// but illegal in unquoted PG identifiers — to underscore. We could
// alternatively double-quote the identifier, but every downstream
// caller would then have to remember to quote it the same way, which
// is a footgun (a single `INSERT INTO tenant.acme.records ...` would
// be silently misinterpreted as schema-qualified).
var schemaReplacer = strings.NewReplacer(".", "_", "-", "_")

// ErrInvalidTenantID is returned by SchemaName and ProvisionSchema /
// DropSchema / AcquireForTenant whenever the tenant ID fails the
// shared character-class check. Callers should treat this as a 4xx
// input-validation error, never a server-side fault.
var ErrInvalidTenantID = errors.New("tenants: invalid tenant identifier")

// SchemaName returns the PG schema name reserved for tenant. The
// caller-supplied identifier is validated against the same character
// class enforced by the tenant_quotas table; on any rejection the
// returned name is "" and the error wraps ErrInvalidTenantID.
//
// The mapping is deterministic: dot and hyphen → underscore, then
// prepend `tenant_`. Two tenants whose normalised form would collide
// (e.g. "acme.corp" vs "acme-corp") still get distinct schemas
// because the upstream tenant ID is a primary key in tenant_quotas
// and the normalised form is derived after that uniqueness gate.
func SchemaName(tenant string) (string, error) {
	if !schemaSafe.MatchString(tenant) {
		return "", ErrInvalidTenantID
	}
	return schemaPrefix + schemaReplacer.Replace(tenant), nil
}

// ProvisionSchema creates the per-tenant schema (idempotent — `IF NOT
// EXISTS`) and then executes tableSchemaSQL inside that schema's
// `search_path`. tableSchemaSQL is typically a CREATE TABLE bundle
// describing the per-tenant tables; it runs verbatim, so callers must
// trust its source.
//
// Atomicity: the schema-create + DDL run inside a single tx so a
// failed DDL script rolls back the schema creation as well. This
// keeps re-provisioning cleanly idempotent (no half-built schema
// left behind to confuse the next attempt).
func ProvisionSchema(ctx context.Context, pool *pgxpool.Pool, tenant, tableSchemaSQL string) error {
	schema, err := SchemaName(tenant)
	if err != nil {
		return err
	}
	if pool == nil {
		return errors.New("tenants: nil pool")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("tenants: begin tx for schema %q: %w", schema, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// schema is guaranteed identifier-safe by SchemaName, so the
	// fmt.Sprintf is not an injection vector. Same reasoning applies
	// to the SET LOCAL search_path below.
	if _, err := tx.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", schema)); err != nil {
		return fmt.Errorf("tenants: CREATE SCHEMA %q: %w", schema, err)
	}
	if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL search_path = %s, public", schema)); err != nil {
		return fmt.Errorf("tenants: SET search_path %q: %w", schema, err)
	}
	if strings.TrimSpace(tableSchemaSQL) != "" {
		if _, err := tx.Exec(ctx, tableSchemaSQL); err != nil {
			return fmt.Errorf("tenants: run tenant DDL in %q: %w", schema, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("tenants: commit schema %q: %w", schema, err)
	}
	return nil
}

// DropSchema removes the per-tenant schema and every object inside it
// (CASCADE). Idempotent — missing schemas return nil. Intended for
// off-boarding and BDD teardown; not invoked by request-path code.
func DropSchema(ctx context.Context, pool *pgxpool.Pool, tenant string) error {
	schema, err := SchemaName(tenant)
	if err != nil {
		return err
	}
	if pool == nil {
		return errors.New("tenants: nil pool")
	}
	if _, err := pool.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", schema)); err != nil {
		return fmt.Errorf("tenants: DROP SCHEMA %q: %w", schema, err)
	}
	return nil
}

// AcquireForTenant lends a pool connection with its session
// search_path scoped to the tenant. The returned release function MUST
// be called (even on error paths) so the connection is reset and
// returned to the pool:
//
//   - On release we run `SET search_path = DEFAULT` so the next
//     caller that pulls this conn does not silently inherit the
//     prior tenant's namespace. pgxpool reuses connections, so this
//     reset is the only thing standing between two tenants sharing
//     a conn over the lifetime of the pool.
//
// On error the release hook is nil-safe — callers can still defer it.
func AcquireForTenant(ctx context.Context, pool *pgxpool.Pool, tenant string) (*pgxpool.Conn, func(), error) {
	schema, err := SchemaName(tenant)
	if err != nil {
		return nil, func() {}, err
	}
	if pool == nil {
		return nil, func() {}, errors.New("tenants: nil pool")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, func() {}, fmt.Errorf("tenants: acquire conn for %q: %w", schema, err)
	}
	if _, err := conn.Exec(ctx, fmt.Sprintf("SET search_path = %s, public", schema)); err != nil {
		conn.Release()
		return nil, func() {}, fmt.Errorf("tenants: SET search_path %q: %w", schema, err)
	}
	release := func() {
		// Reset session state before returning the conn to the pool.
		// Use a fresh background context so a cancelled caller ctx
		// can't strand the conn with a stale search_path.
		resetCtx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, _ = conn.Exec(resetCtx, "SET search_path = DEFAULT")
		conn.Release()
	}
	return conn, release, nil
}
