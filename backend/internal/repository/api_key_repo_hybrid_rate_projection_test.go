package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestGetByKeyForAuthCarriesHybridSubscriptionRate(t *testing.T) {
	repo, client := newAPIKeyRepoSQLite(t)
	ctx := context.Background()
	user := mustCreateAPIKeyRepoUser(t, ctx, client, "hybrid-rate-auth@test.com")
	group, err := client.Group.Create().
		SetName("hybrid-rate-auth").
		SetPlatform(service.PlatformOpenAI).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscriptionBalance).
		SetRateMultiplier(0.25).
		SetSubscriptionRateMultiplier(1).
		Save(ctx)
	require.NoError(t, err)

	key := &service.APIKey{UserID: user.ID, Key: "sk-hybrid-rate-auth", Name: "hybrid", GroupID: &group.ID, Status: service.StatusActive}
	require.NoError(t, repo.Create(ctx, key))

	loaded, err := repo.GetByKeyForAuth(ctx, key.Key)
	require.NoError(t, err)
	require.NotNil(t, loaded.Group)
	require.Equal(t, 0.25, loaded.Group.RateMultiplier)
	require.NotNil(t, loaded.Group.SubscriptionRateMultiplier)
	require.Equal(t, 1.0, loaded.Group.SubscriptionBillingRateMultiplier())
}
