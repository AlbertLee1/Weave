-- US-438 Tenants 计费集成与预警: per-tenant per-metric monthly usage
-- counters, a derived view that joins them against tenant_quotas for
-- alert evaluation, and a dedup table so each (tenant, month, metric,
-- threshold) alert fires at most once per calendar month.
--
-- Metric vocabulary (TEXT, lowercase) matches pkg/tenants:
--   'objects'   total object count cap (storage rows)
--   'storage'   total bytes-on-disk
--   'requests'  request count (informational; QPS limiting still rides
--               the live token bucket in pkg/tenants.Manager)

CREATE TABLE IF NOT EXISTS tenant_monthly_usage (
    tenant     TEXT NOT NULL,
    month      DATE NOT NULL,
    metric     TEXT NOT NULL,
    amount     BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_monthly_usage_pkey PRIMARY KEY (tenant, month, metric)
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_monthly_usage_metric_enum') THEN
        ALTER TABLE tenant_monthly_usage
            ADD CONSTRAINT tenant_monthly_usage_metric_enum
            CHECK (metric IN ('objects','storage','requests'));
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_monthly_usage_amount_nonneg') THEN
        ALTER TABLE tenant_monthly_usage
            ADD CONSTRAINT tenant_monthly_usage_amount_nonneg
            CHECK (amount >= 0);
    END IF;
END$$;

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_monthly_usage_month_first') THEN
        ALTER TABLE tenant_monthly_usage
            ADD CONSTRAINT tenant_monthly_usage_month_first
            CHECK (date_trunc('month', month) = month);
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_tenant_monthly_usage_month
    ON tenant_monthly_usage(month);

-- Per-tenant per-month dedup ledger so an 80%/100% alert is dispatched
-- at most once per month per metric. Alerts cleared at month rollover
-- because the row key includes month.
CREATE TABLE IF NOT EXISTS tenant_quota_alerts (
    tenant     TEXT NOT NULL,
    month      DATE NOT NULL,
    metric     TEXT NOT NULL,
    threshold  INTEGER NOT NULL,
    sent_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT tenant_quota_alerts_pkey PRIMARY KEY (tenant, month, metric, threshold)
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'tenant_quota_alerts_threshold_range') THEN
        ALTER TABLE tenant_quota_alerts
            ADD CONSTRAINT tenant_quota_alerts_threshold_range
            CHECK (threshold IN (80, 100));
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_tenant_quota_alerts_month
    ON tenant_quota_alerts(month);

-- tenant_usage_monthly: derived view exposing the current calendar
-- month's per-metric usage joined with the configured tenant_quotas
-- caps. Reading this view is the canonical "what is this tenant
-- consuming right now and how close are they to the limit" surface.
--
-- LEFT JOIN of quotas ⨝ usage so a tenant with a quota but no usage row
-- yet still surfaces (amount=0, percent=0). RIGHT-side coalesce keeps
-- the tenant_usage_monthly row stable even if the metric has never been
-- recorded.
CREATE OR REPLACE VIEW tenant_usage_monthly AS
WITH metrics(metric) AS (
    VALUES ('objects'), ('storage'), ('requests')
),
current_month AS (
    SELECT date_trunc('month', NOW())::DATE AS month
)
SELECT
    q.tenant            AS tenant,
    cm.month            AS month,
    m.metric            AS metric,
    COALESCE(u.amount, 0)::BIGINT AS amount,
    CASE m.metric
        WHEN 'objects'  THEN q.max_objects
        WHEN 'storage'  THEN q.max_storage
        ELSE 0::BIGINT
    END                  AS cap,
    CASE
        WHEN (
            CASE m.metric
                WHEN 'objects' THEN q.max_objects
                WHEN 'storage' THEN q.max_storage
                ELSE 0::BIGINT
            END
        ) <= 0 THEN 0
        ELSE LEAST(
            100,
            (COALESCE(u.amount, 0) * 100 / NULLIF(
                CASE m.metric
                    WHEN 'objects' THEN q.max_objects
                    WHEN 'storage' THEN q.max_storage
                    ELSE 0::BIGINT
                END, 0
            ))::INTEGER
        )
    END                  AS percent
FROM tenant_quotas q
CROSS JOIN current_month cm
CROSS JOIN metrics m
LEFT JOIN tenant_monthly_usage u
       ON u.tenant = q.tenant
      AND u.month  = cm.month
      AND u.metric = m.metric;
