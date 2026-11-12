package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSettleHybridUsageLogUsesCommittedWalletCost(t *testing.T) {
	subID := int64(4)
	usage := &service.UsageLog{
		BillingType:    service.BillingTypeSubscription,
		SubscriptionID: &subID,
		ActualCost:     0.34444,
		RateMultiplier: 1,
	}
	cmd := &service.UsageBillingCommand{
		HybridGroupID:             3,
		HybridSplitPricing:        true,
		HybridBalanceCostUSD:      0.08611,
		BalanceCost:               0.08611,
		HybridSubscriptionCostUSD: 0.34444,
	}
	result := &service.UsageBillingApplyResult{BillingType: service.BillingTypeBalance}

	settleHybridUsageLog(usage, cmd, result)

	require.Equal(t, service.BillingTypeBalance, usage.BillingType)
	require.Nil(t, usage.SubscriptionID)
	require.Equal(t, 0.08611, usage.ActualCost)
	require.InDelta(t, 0.25, usage.RateMultiplier, 1e-12)
}

func TestSettleHybridUsageLogKeepsCommittedSubscriptionCost(t *testing.T) {
	subID := int64(4)
	usage := &service.UsageLog{
		BillingType:    service.BillingTypeSubscription,
		SubscriptionID: &subID,
		ActualCost:     0.34444,
		RateMultiplier: 1,
	}
	cmd := &service.UsageBillingCommand{HybridGroupID: 3, HybridSplitPricing: true, SubscriptionCost: 0.34444}
	result := &service.UsageBillingApplyResult{BillingType: service.BillingTypeSubscription, SubscriptionID: &subID}

	settleHybridUsageLog(usage, cmd, result)

	require.Equal(t, service.BillingTypeSubscription, usage.BillingType)
	require.Equal(t, &subID, usage.SubscriptionID)
	require.Equal(t, 0.34444, usage.ActualCost)
	require.Equal(t, 1.0, usage.RateMultiplier)
}
