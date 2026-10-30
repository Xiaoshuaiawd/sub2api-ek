//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelIntelligenceForwardMappingKeepsConfiguredPricing(t *testing.T) {
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			channel := Channel{
				ID: 7, Status: StatusActive, GroupIDs: []int64{10}, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"public-model": "priced-model"}},
			}
			svc := newTestChannelService(makeStandardRepo(channel, map[int64]string{10: PlatformOpenAI}))
			groupID := int64(10)
			normal := svc.ResolveChannelMapping(context.Background(), groupID, "public-model")
			require.True(t, normal.Mapped)
			require.Equal(t, "priced-model", normal.MappedModel)
			require.Equal(t, "priced-model", normal.ToUsageFields("public-model", "account-model").ChannelMappedModel)

			ctx := WithIntelligenceRouting(context.Background())
			for _, get := range []func() ChannelMappingResult{
				func() ChannelMappingResult { return svc.ResolveChannelMapping(ctx, groupID, "public-model") },
				func() ChannelMappingResult {
					result, _ := svc.ResolveChannelMappingAndRestrict(ctx, &groupID, "public-model")
					return result
				},
			} {
				result := get()
				require.False(t, result.Mapped, "the channel mapping must not reach the upstream account")
				require.Equal(t, "public-model", result.MappedModel)
				require.Equal(t, int64(7), result.ChannelID)
				require.Equal(t, source, result.BillingModelSource)
				fields := result.ToUsageFields("public-model", "account-model")
				require.Equal(t, "priced-model", fields.ChannelMappedModel, "retain configured pricing basis")
				require.Equal(t, "public-model→account-model", fields.ModelMappingChain, "record only mappings actually forwarded")
			}
		})
	}
}

func TestChannelIntelligenceRestrictionUsesConfiguredPricingMapping(t *testing.T) {
	for _, source := range []string{BillingModelSourceChannelMapped, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			channel := Channel{
				ID: 7, Status: StatusActive, GroupIDs: []int64{10}, RestrictModels: true,
				BillingModelSource: source,
				ModelMapping:       map[string]map[string]string{PlatformOpenAI: {"public-model": "priced-model"}},
				ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"priced-model"}}},
			}
			channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{10: PlatformOpenAI}))
			groupID := int64(10)
			ctx := WithIntelligenceRouting(context.Background())
			gateway := &GatewayService{channelService: channelSvc}
			openai := &OpenAIGatewayService{channelService: channelSvc}
			require.False(t, gateway.checkChannelPricingRestriction(ctx, &groupID, "public-model"))
			require.False(t, openai.checkChannelPricingRestriction(ctx, &groupID, "public-model"))
			require.True(t, gateway.checkChannelPricingRestriction(ctx, &groupID, "not-priced"))
			require.True(t, openai.checkChannelPricingRestriction(ctx, &groupID, "not-priced"))
		})
	}
}

func TestChannelIntelligenceInflightEstimatePreservesPricingAndUsesActualForwardInput(t *testing.T) {
	groupID := int64(10)
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			channel := Channel{
				ID: 7, Status: StatusActive, GroupIDs: []int64{groupID}, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"public-model": "priced-model"}},
			}
			channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformOpenAI}))
			deps := inflightEstimateDeps{resolveMapping: channelSvc.ResolveChannelMapping}
			apiKey := &APIKey{GroupID: &groupID}
			ctx := WithIntelligenceRouting(context.Background())
			primary, fallback, upstreamInput, probe := inflightBillingModelCandidates(ctx, deps, apiKey, "public-model")
			require.Equal(t, "public-model", upstreamInput, "account-level mapping must start from the actually forwarded model")
			require.Equal(t, source == BillingModelSourceUpstream || source == BillingModelSourceResponse, probe)
			if source == BillingModelSourceRequested {
				require.Equal(t, []string{"public-model"}, primary)
				require.Equal(t, []string{"priced-model"}, fallback)
			} else {
				require.Equal(t, []string{"priced-model", "public-model"}, primary)
				require.Empty(t, fallback)
			}
		})
	}
}

func TestChannelIntelligenceInflightReservationUsesChannelPrice(t *testing.T) {
	groupID := int64(10)
	channel := Channel{
		ID: 7, Status: StatusActive, GroupIDs: []int64{groupID}, BillingModelSource: BillingModelSourceChannelMapped,
		ModelMapping: map[string]map[string]string{PlatformAnthropic: {"private-alias": "claude-sonnet-4-5"}},
	}
	channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformAnthropic}))
	gateway := newInflightEstimateGateway(t, channelSvc)
	apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformAnthropic, RateMultiplier: 1}}
	req := InflightEstimateRequest{Model: "private-alias", BodyBytes: 4000, MaxTokens: 1000}
	ordinary, ordinaryPriced := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
	intelligence, intelligencePriced := gateway.EstimateInflightReservation(WithIntelligenceRouting(context.Background()), apiKey, req)
	require.True(t, ordinaryPriced)
	require.True(t, intelligencePriced)
	require.Greater(t, ordinary, 0.0)
	require.InDelta(t, ordinary, intelligence, 1e-12, "skipping the forwarding mapping must retain its configured reservation price")
}

func TestChannelIntelligenceInflightReservationUsesEligibleUpstreamPrices(t *testing.T) {
	groupID := int64(10)
	for _, source := range []string{BillingModelSourceRequested, BillingModelSourceChannelMapped, BillingModelSourceUpstream, BillingModelSourceResponse} {
		t.Run(source, func(t *testing.T) {
			cheap, testPrice, ordinaryPrice := 1e-6, 10e-6, 100e-6
			channel := Channel{
				ID: 7, Status: StatusActive, GroupIDs: []int64{groupID}, BillingModelSource: source,
				ModelMapping: map[string]map[string]string{PlatformOpenAI: {"public-model": "priced-model"}},
				ModelPricing: []ChannelModelPricing{
					{Platform: PlatformOpenAI, Models: []string{"priced-model"}, InputPrice: &cheap, OutputPrice: &cheap},
					{Platform: PlatformOpenAI, Models: []string{"iq-actual"}, InputPrice: &testPrice, OutputPrice: &testPrice},
					{Platform: PlatformOpenAI, Models: []string{"ordinary-actual"}, InputPrice: &ordinaryPrice, OutputPrice: &ordinaryPrice},
				},
			}
			channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformOpenAI}))
			gateway := newInflightEstimateGateway(t, channelSvc)
			snapshot := &inflightSnapshotCacheStub{byBucket: map[string][]Account{inflightBucketKey(groupID, PlatformOpenAI): {
				{ID: 1, Platform: PlatformOpenAI, Extra: map[string]any{IntelligenceRoutingExtraKey: true}, Credentials: map[string]any{"model_mapping": map[string]any{"public-model": "iq-actual"}}},
				{ID: 2, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": map[string]any{"public-model": "ordinary-actual"}}},
			}}}
			repo := attachInflightSnapshot(gateway, snapshot)
			apiKey := &APIKey{User: &User{ID: 1}, GroupID: &groupID, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}}
			req := InflightEstimateRequest{Model: "public-model", BodyBytes: 4000, MaxTokens: 1000}
			ordinary, ok := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
			require.True(t, ok)
			require.Zero(t, snapshot.reads.Load(), "ordinary priced requests retain the existing no-snapshot path")
			req.Model = "iq-actual"
			actualCost, ok := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
			require.True(t, ok)
			require.Greater(t, actualCost, ordinary)
			req.Model = "ordinary-actual"
			ineligibleCost, ok := gateway.EstimateInflightReservation(context.Background(), apiKey, req)
			require.True(t, ok)
			require.Greater(t, ineligibleCost, actualCost)
			req.Model = "public-model"
			iqCost, ok := gateway.EstimateInflightReservation(WithIntelligenceRouting(context.Background()), apiKey, req)
			require.True(t, ok)
			if source == BillingModelSourceUpstream || source == BillingModelSourceResponse {
				require.InDelta(t, actualCost, iqCost, 1e-12, "reserve for eligible account's actual upstream model")
				require.Greater(t, snapshot.reads.Load(), int64(0))
			} else {
				require.InDelta(t, ordinary, iqCost, 1e-12, "fixed channel/requested price remains unchanged")
				require.Zero(t, snapshot.reads.Load())
			}
			require.Zero(t, repo.dbCalls.Load(), "priced IQ requests must use the existing scheduler snapshot")
		})
	}
}
