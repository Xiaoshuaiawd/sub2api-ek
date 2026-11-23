//go:build unit

package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestSubscriptionCacheRejectsLateFillAfterInvalidation(t *testing.T) {
	ctx := context.Background()
	cache, _ := newMiniRedisCache(t)
	userID, groupID := int64(5), int64(7)
	generation, err := cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	old := &service.SubscriptionCacheData{
		SubscriptionID: 10, Status: "active", ExpiresAt: time.Now().Add(time.Hour), Version: 1, DailyUsage: 4,
	}
	require.NoError(t, cache.InvalidateSubscriptionCache(ctx, userID, groupID))
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, old, generation))
	_, err = cache.GetSubscriptionCache(ctx, userID, groupID)
	require.ErrorIs(t, err, redis.Nil)

	current, err := cache.GetSubscriptionCacheGeneration(ctx, userID, groupID)
	require.NoError(t, err)
	require.NotEqual(t, generation, current)
	fresh := *old
	fresh.Version, fresh.DailyUsage = 2, 1
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, &fresh, current))
	require.NoError(t, cache.SetSubscriptionCacheIfGeneration(ctx, userID, groupID, old, current))
	got, err := cache.GetSubscriptionCache(ctx, userID, groupID)
	require.NoError(t, err)
	require.Equal(t, fresh.SubscriptionID, got.SubscriptionID)
	require.Equal(t, fresh.Version, got.Version)
	require.Equal(t, 1.0, got.DailyUsage)
}
