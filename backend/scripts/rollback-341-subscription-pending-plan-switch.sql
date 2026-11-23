-- Run manually after every application instance has rolled back.
-- Leaving the columns in place preserves pending purchases for a later redeploy.
-- Back up any non-NULL pending values before choosing to remove the columns.
BEGIN;

ALTER TABLE user_subscriptions
    DROP COLUMN IF EXISTS pending_plan_id,
    DROP COLUMN IF EXISTS pending_daily_limit_usd,
    DROP COLUMN IF EXISTS pending_weekly_limit_usd,
    DROP COLUMN IF EXISTS pending_monthly_limit_usd;

-- Allow the forward migration to run if the feature is deployed again.
DELETE FROM schema_migrations WHERE filename = '341_subscription_pending_plan_switch.sql';

COMMIT;
