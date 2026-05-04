-- US-429 Notification Delivery (Email + Slack + Webhook): per-user
-- per-channel delivery preferences. The dispatcher reads this table to
-- route a single Activity into N concrete deliveries (e.g. SMTP +
-- Slack + Webhook). Absence of any row for (user_id, channel) means the
-- channel is INACTIVE for that user — explicit opt-in semantics.
--
-- Channel values:
--   'email'    SMTP delivery; target empty → use the resolver's default
--              email address (from auth.UserRepository).
--   'slack'    Slack incoming webhook; target = full webhook URL.
--   'webhook'  Generic JSON webhook; target = endpoint URL.
--
-- target is a free-form delivery destination — meaning depends on
-- channel. Empty for 'email' is permissible because the address is
-- resolved at dispatch time; non-empty optional override is permitted
-- for users who prefer a non-primary address.
CREATE TABLE IF NOT EXISTS notification_preferences (
    user_id    TEXT NOT NULL,
    channel    TEXT NOT NULL,
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    target     TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT notification_preferences_pkey PRIMARY KEY (user_id, channel)
);

DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'notification_preferences_channel_enum') THEN
        ALTER TABLE notification_preferences
            ADD CONSTRAINT notification_preferences_channel_enum
            CHECK (channel IN ('email','slack','webhook'));
    END IF;
END$$;

CREATE INDEX IF NOT EXISTS idx_notification_preferences_user_enabled
    ON notification_preferences(user_id) WHERE enabled = TRUE;
