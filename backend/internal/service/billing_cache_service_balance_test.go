//go:build unit

package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type balanceEligibilityCacheStub struct {
	billingCacheWorkerStub

	balance                  float64
	cacheMissAfterInvalidate bool
	invalidated              atomic.Bool
	deductCalls              atomic.Int64
	invalidateCalls          atomic.Int64
}

type hybridEligibilityCacheStub struct {
	*balanceEligibilityCacheStub
	subscription *SubscriptionCacheData
}

func (s *hybridEligibilityCacheStub) GetSubscriptionCache(context.Context, int64, int64) (*SubscriptionCacheData, error) {
	return s.subscription, nil
}

func TestCheckBillingEligibility_HybridFallsBackWhenSubscriptionCacheIsExhausted(t *testing.T) {
	limit := 1.0
	cache := &hybridEligibilityCacheStub{
		balanceEligibilityCacheStub: &balanceEligibilityCacheStub{balance: 2},
		subscription: &SubscriptionCacheData{
			SubscriptionID: 20, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour), DailyUsage: limit,
		},
	}
	group := &Group{ID: 10, SubscriptionType: SubscriptionTypeSubscriptionBalance, DailyLimitUSD: &limit}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, &UserSubscription{ID: 20}, ""))

	cache.balance = 0
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, &UserSubscription{ID: 20}, ""), ErrInsufficientBalance)
}

func TestCheckBillingEligibilityReloadsSubscriptionWhenCachedOwnerDiffers(t *testing.T) {
	now := time.Now()
	limit := 5.0
	sub := &UserSubscription{ID: 20, Status: SubscriptionStatusActive, ExpiresAt: now.Add(time.Hour), UpdatedAt: now, DailyUsageUSD: 1}
	cache := &hybridEligibilityCacheStub{
		balanceEligibilityCacheStub: &balanceEligibilityCacheStub{},
		subscription: &SubscriptionCacheData{
			SubscriptionID: 19, Status: SubscriptionStatusActive, ExpiresAt: sub.ExpiresAt, DailyUsage: 5, Version: now.UnixMicro(),
		},
	}
	repo := &fixedChargeSubscriptionRepoStub{sub: sub}
	svc := NewBillingCacheService(cache, nil, repo, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	group := &Group{ID: 10, SubscriptionType: SubscriptionTypeSubscription, DailyLimitUSD: &limit}
	require.NoError(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, sub, ""))

	// If the authentication snapshot itself is stale, fail instead of billing
	// the previous owner or pairing its limits with fresh usage.
	stale := *sub
	stale.ID = 19
	cache.subscription = nil
	require.ErrorIs(t, svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, group, &stale, ""), ErrSubscriptionInvalid)
}

func (s *balanceEligibilityCacheStub) GetUserBalance(context.Context, int64) (float64, error) {
	if s.cacheMissAfterInvalidate && s.invalidated.Load() {
		return 0, errors.New("cache miss")
	}
	return s.balance, nil
}

func (s *balanceEligibilityCacheStub) DeductUserBalance(context.Context, int64, float64) error {
	s.deductCalls.Add(1)
	return nil
}

func (s *balanceEligibilityCacheStub) InvalidateUserBalance(context.Context, int64) error {
	s.invalidateCalls.Add(1)
	s.invalidated.Store(true)
	return nil
}

func TestCheckBillingEligibility_RejectsBalanceBelowMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.005}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
}

func TestCheckBillingEligibility_AllowsBalanceAtMinimumReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.01}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.NoError(t, err)
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesExhaustedBalance(t *testing.T) {
	cache := &balanceEligibilityCacheStub{
		balance:                  0.50,
		cacheMissAfterInvalidate: true,
	}
	userRepo := &balanceLoadUserRepoStub{balance: -0.25}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, userRepo, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := -0.25
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.75},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{
		NewBalance:         &newBalance,
		BalanceOverdrafted: true,
	})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())

	err := svc.CheckBillingEligibility(context.Background(), &User{ID: 1}, nil, nil, nil, "")
	require.ErrorIs(t, err, ErrInsufficientBalance)
	require.Equal(t, int64(1), userRepo.calls.Load())
}

func TestSyncBalanceCacheAfterDeduction_InvalidatesWhenBalanceFallsBelowReserve(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0.50}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.005
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.495},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(1), cache.invalidateCalls.Load())
	require.Equal(t, int64(0), cache.deductCalls.Load())
}

func TestSyncBalanceCacheAfterDeduction_QueuesDeductWhenBalanceStillEligible(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 1}
	cfg := &config.Config{}
	cfg.Billing.MinimumBalanceReserve = 0.01
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(svc.Stop)

	newBalance := 0.75
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{
		Cost: &CostBreakdown{ActualCost: 0.25},
		User: &User{ID: 1},
	}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &newBalance})

	require.Equal(t, int64(0), cache.invalidateCalls.Load())
	require.Eventually(t, func() bool {
		return cache.deductCalls.Load() == 1
	}, 2*time.Second, 10*time.Millisecond)
}

func TestSyncBalanceCacheAfterAutoCreditInvalidatesInsteadOfDeducting(t *testing.T) {
	cache := &balanceEligibilityCacheStub{balance: 0}
	svc := NewBillingCacheService(cache, nil, nil, nil, nil, nil, &config.Config{}, nil)
	t.Cleanup(svc.Stop)
	balance := 10.0
	syncBalanceCacheAfterDeduction(context.Background(), &postUsageBillingParams{Cost: &CostBreakdown{ActualCost: 0.25}, User: &User{ID: 1}}, &billingDeps{billingCacheService: svc}, &UsageBillingApplyResult{NewBalance: &balance, AutoCreditIssued: true})
	require.EqualValues(t, 1, cache.invalidateCalls.Load())
	require.Zero(t, cache.deductCalls.Load())
}
