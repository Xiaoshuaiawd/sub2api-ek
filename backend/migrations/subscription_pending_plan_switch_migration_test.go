package migrations

import (
	"strings"
	"testing"
)

// 341 只加可空列，不 backfill：老订阅、老订单这四列为 NULL 时行为必须与加列前一致。
func TestSubscriptionPendingPlanSwitchMigrationOnlyAddsNullableColumns(t *testing.T) {
	content, err := FS.ReadFile("341_subscription_pending_plan_switch.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(content)
	for _, expected := range []string{
		"ADD COLUMN IF NOT EXISTS pending_plan_id          BIGINT",
		"ADD COLUMN IF NOT EXISTS pending_daily_limit_usd",
		"ADD COLUMN IF NOT EXISTS pending_weekly_limit_usd",
		"ADD COLUMN IF NOT EXISTS pending_monthly_limit_usd",
		"DEFAULT NULL",
	} {
		if !strings.Contains(sql, expected) {
			t.Fatalf("migration missing %q", expected)
		}
	}
	// 不允许出现默认值/回填：有默认值就意味着老行会被赋上新语义。
	for _, forbidden := range []string{"DEFAULT 0", "DEFAULT FALSE", "UPDATE user_subscriptions", "DROP COLUMN"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("migration must not contain %q", forbidden)
		}
	}
}
