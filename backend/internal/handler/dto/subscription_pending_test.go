package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUserSubscriptionPendingQuotaJSON(t *testing.T) {
	planID := int64(4)
	current, pending := 5.0, 2.0
	sub := &service.UserSubscription{
		PlanID: &planID, DailyLimitUSD: &current,
		PendingPlanID: &planID, PendingPlanName: "Starter", PendingDailyLimitUSD: &pending,
	}
	encode := func() map[string]any {
		raw, err := json.Marshal(UserSubscriptionFromService(sub))
		require.NoError(t, err)
		var got map[string]any
		require.NoError(t, json.Unmarshal(raw, &got))
		return got
	}
	got := encode()
	require.Equal(t, 5.0, got["daily_limit_usd"])
	require.Equal(t, 4.0, got["pending_plan_id"])
	require.Equal(t, "Starter", got["pending_plan_name"])
	require.Equal(t, 2.0, got["pending_daily_limit_usd"])
	require.NotContains(t, got, "pending_weekly_limit_usd")
	require.NotContains(t, got, "pending_monthly_limit_usd")

	sub.ClearPendingPlanLimits()
	for key := range encode() {
		require.NotContains(t, key, "pending_")
	}
}
