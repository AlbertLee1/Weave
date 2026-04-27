-- US-277 Tenant Quotas: per-tenant resource caps for multi-tenant SaaS.
--
-- One row per tenant. tenant is sourced from auth.User.Attributes["realm"]
-- (the same JWT claim feature flags use). Every limit column defaults to
-- 0 = "no limit" so a freshly inserted row is permissive until an
-- operator dials it down. See pkg/tenants/manager.go for the evaluation
-- rules.

CREATE TABLE IF NOT EXISTS tenant_quotas (
    tenant       TEXT PRIMARY KEY,
    max_objects  BIGINT NOT NULL DEFAULT 0,
    max_storage  BIGINT NOT NULL DEFAULT 0,
    max_qps      DOUBLE PRECISION NOT NULL DEFAULT 0,
    burst        INTEGER NOT NULL DEFAULT 0,
    description  TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_quotas_tenant_format') THEN
        ALTER TABLE tenant_quotas
            ADD CONSTRAINT tenant_quotas_tenant_format
            CHECK (tenant ~ '^[A-Za-z0-9._-]{1,128}$');
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_quotas_nonnegative') THEN
        ALTER TABLE tenant_quotas
            ADD CONSTRAINT tenant_quotas_nonnegative
            CHECK (max_objects >= 0 AND max_storage >= 0 AND max_qps >= 0 AND burst >= 0);
    END IF;
END$$;
