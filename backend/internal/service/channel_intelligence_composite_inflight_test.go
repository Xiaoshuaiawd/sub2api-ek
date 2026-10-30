//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceCompositeInflightUsesResolvedRouteAndChannelPrice(t *testing.T) {
	const (
		public = "public-alias"
		route  = "grok-route"
		price  = "channel-priced"
	)
	groupID := int64(810)
	apiKey := &APIKey{GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite}}
	ctx := WithIntelligenceRouting(WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{
		Matched: true, PublicModel: public, TargetPlatform: PlatformGrok, UpstreamModel: route,
	}))
	for _, source := range []string{BillingModelSourceChannelMapped, BillingModelSourceRequested, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			ch := Channel{ID: 811, Status: StatusActive, GroupIDs: []int64{groupID}, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformGrok: {route: price}}}
			channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{groupID: PlatformComposite}))
			primary, fallbacks, upstreamInput, probe := inflightBillingModelCandidates(ctx, inflightEstimateDeps{resolveMapping: channelSvc.ResolveChannelMapping}, apiKey, public)
			require.Equal(t, route, upstreamInput, "account mappings must start from the concrete composite route")
			require.Equal(t, source == BillingModelSourceUpstream || source == BillingModelSourceResponse, probe)
			if source == BillingModelSourceRequested {
				require.Equal(t, []string{public}, primary, "requested pricing keeps the public model as the primary basis")
				require.Equal(t, []string{price, route}, fallbacks)
			} else {
				require.Equal(t, []string{price, public}, primary, "channel pricing resolves against the concrete forwarding model")
				require.Equal(t, []string{route}, fallbacks)
			}

			// Keep existing ordinary-request behaviour unchanged.
			ordinaryCtx := WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: public, TargetPlatform: PlatformGrok, UpstreamModel: route})
			ordinary, _, ordinaryInput, _ := inflightBillingModelCandidates(ordinaryCtx, inflightEstimateDeps{resolveMapping: channelSvc.ResolveChannelMapping}, apiKey, public)
			require.Equal(t, public, ordinaryInput)
			require.Equal(t, []string{public}, ordinary)
		})
	}

}

func TestIntelligenceCompositeInflightUsesResolvedPlatformAndEligibleAccountPrice(t *testing.T) {
	const (
		public = "public-alias"
		route  = "grok-route"
		price  = "channel-priced"
		actual = "iq-actual"
	)
	groupID := int64(820)
	cheap, expensive, ordinaryPrice := 1e-6, 10e-6, 100e-6
	ch := Channel{ID: 821, Status: StatusActive, GroupIDs: []int64{groupID}, BillingModelSource: BillingModelSourceUpstream,
		ModelMapping: map[string]map[string]string{PlatformGrok: {route: price}},
		ModelPricing: []ChannelModelPricing{
			{Platform: PlatformGrok, Models: []string{price, route}, InputPrice: &cheap, OutputPrice: &cheap},
			{Platform: PlatformGrok, Models: []string{actual}, InputPrice: &expensive, OutputPrice: &expensive},
			{Platform: PlatformGrok, Models: []string{"ordinary-actual"}, InputPrice: &ordinaryPrice, OutputPrice: &ordinaryPrice},
		}}
	channelSvc := newTestChannelService(makeStandardRepo(ch, map[int64]string{groupID: PlatformComposite}))
	cfg := &config.Config{}
	cfg.Billing.InflightReservation = config.InflightReservationConfig{Enabled: true, DefaultMaxTokens: 1000, MaxInputTokens: 200000, MaxOutputTokens: 128000}
	billing := NewBillingService(cfg, nil)
	snapshot := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(groupID, PlatformGrok): {
		{ID: 1, Platform: PlatformGrok, Extra: map[string]any{IntelligenceRoutingExtraKey: true}, Credentials: map[string]any{"model_mapping": map[string]any{route: actual}}},
		{ID: 2, Platform: PlatformGrok, Credentials: map[string]any{"model_mapping": map[string]any{route: "ordinary-actual"}}},
	}}}
	repo := &inflightCountingAccountRepo{}
	svc := &OpenAIGatewayService{cfg: cfg, billingService: billing, resolver: NewModelPricingResolver(channelSvc, billing), channelService: channelSvc,
		schedulerSnapshot: NewSchedulerSnapshotService(snapshot, nil, repo, nil, cfg)}
	deps := svc.inflightEstimateDeps()
	deps.userGroupRate = nil
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformComposite, RateMultiplier: 1}}
	ctx := WithIntelligenceRouting(WithCompositeRouteDecision(context.Background(), CompositeRouteDecision{Matched: true, PublicModel: public, TargetPlatform: PlatformGrok, UpstreamModel: route}))
	req := InflightEstimateRequest{Model: public, BodyBytes: 4000, MaxTokens: 1000}
	cost, ok := deps.estimate(ctx, apiKey, req)
	require.True(t, ok)
	want := deps.estimateOne(ctx, apiKey, actual, req, 1, 1)
	require.Greater(t, want, 0.0)
	require.InDelta(t, want, cost, 1e-12, "reserve for qualified Grok account mapping, excluding ordinary accounts")
	require.Greater(t, snapshot.reads.Load(), int64(0))
	require.Zero(t, repo.dbCalls.Load(), "use the existing scheduler snapshot")
}
