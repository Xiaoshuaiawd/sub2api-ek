//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type recoveryCacheFailure struct{ BillingCache }

func (*recoveryCacheFailure) InvalidateUserBalance(_ context.Context, user int64) error {
	if user == 1 {
		return errors.New("injected cache failure")
	}
	return nil
}

type recoveryCacheRepo struct {
	UsageBillingRepository
	seen     map[string]bool
	acked    []string
	deferred []string
}

func (*recoveryCacheRepo) StageUsageBilling(context.Context, *UsageBillingCommand, *UsageLog) error {
	return nil
}
func (*recoveryCacheRepo) PendingUsageBilling(context.Context, int) ([]UsageBillingEventKey, error) {
	return []UsageBillingEventKey{{RequestID: "first", APIKeyID: 1}, {RequestID: "second", APIKeyID: 2}}, nil
}
func (r *recoveryCacheRepo) ApplyStagedUsageBilling(_ context.Context, key UsageBillingEventKey) (*UsageBillingRecovery, error) {
	fresh := !r.seen[key.RequestID]
	r.seen[key.RequestID] = true
	return &UsageBillingRecovery{Event: UsageBillingEvent{Command: UsageBillingCommand{UserID: key.APIKeyID, BalanceCost: 1}},
		Result: UsageBillingApplyResult{Applied: fresh}, NeedsCache: true}, nil
}
func (r *recoveryCacheRepo) AcknowledgeUsageBillingCache(_ context.Context, key UsageBillingEventKey) error {
	r.acked = append(r.acked, key.RequestID)
	return nil
}
func (r *recoveryCacheRepo) DeferUsageBillingCache(_ context.Context, key UsageBillingEventKey) error {
	r.deferred = append(r.deferred, key.RequestID)
	return nil
}

func TestUsageRecoveryCacheFailureDoesNotStopAnotherEventOrReplayDeltas(t *testing.T) {
	for _, flusher := range []bool{false, true} {
		r := &recoveryCacheRepo{seen: map[string]bool{}}
		cfg := &config.Config{}
		cfg.Database.UserPlatformQuotaFlusherEnabled = flusher
		cache := &BillingCacheService{cache: &recoveryCacheFailure{}, cfg: cfg}
		for attempt := 0; attempt < 2; attempt++ {
			require.Error(t, recoverUsageBillingBatch(context.Background(), r, cache, nil))
		}
		require.Equal(t, []string{"second", "second"}, r.acked)
		require.Equal(t, []string{"first", "first"}, r.deferred)
		require.Len(t, r.seen, 2)
		// The embedded nil BillingCache panics on a monetary delta method. This
		// path only invalidates, under both legacy flusher configurations.
	}
}

type subscriptionInvalidationTestCache struct {
	billingCacheWorkerStub
	groups   []int64
	balance  []int64
	notices  []string
	failOnce int64
}

func (c *subscriptionInvalidationTestCache) InvalidateUserBalance(_ context.Context, userID int64) error {
	c.balance = append(c.balance, userID)
	return nil
}

func (c *subscriptionInvalidationTestCache) InvalidateSubscriptionCache(_ context.Context, _, groupID int64) error {
	c.groups = append(c.groups, groupID)
	if groupID == c.failOnce {
		c.failOnce = 0
		return errors.New("injected subscription cache failure")
	}
	return nil
}

func (c *subscriptionInvalidationTestCache) PublishSubscriptionCacheInvalidation(_ context.Context, key string) error {
	c.notices = append(c.notices, key)
	return nil
}

func (*subscriptionInvalidationTestCache) SubscribeSubscriptionCacheInvalidation(context.Context, func(string)) error {
	return nil
}

func TestUsageRecoveryInvalidatesSharedSubscriptionAfterHybridWindowRollAndWalletFallback(t *testing.T) {
	groupID, newBalance := int64(7), 20.0
	recovery := &UsageBillingRecovery{
		NeedsCache: true,
		Event: UsageBillingEvent{
			// A replay reads the command as staged before the wallet was charged.
			Command: UsageBillingCommand{UserID: 42, HybridGroupID: groupID, SubscriptionCost: 2},
			Usage:   UsageLog{GroupID: &groupID},
		},
		Result: UsageBillingApplyResult{
			BillingType: BillingTypeBalance, NewBalance: &newBalance, SubscriptionWindowReset: true,
			SubscriptionCacheGroupIDs: []int64{7, 9, 7},
		},
	}
	cache := &subscriptionInvalidationTestCache{}
	require.NoError(t, invalidateDurableUsageBilling(context.Background(), recovery, &BillingCacheService{cache: cache}, nil))
	require.Equal(t, []int64{42}, cache.balance)
	require.Equal(t, []int64{7, 9}, cache.groups)
	require.Equal(t, []string{subCacheKey(42, 7), subCacheKey(42, 9)}, cache.notices)
}
