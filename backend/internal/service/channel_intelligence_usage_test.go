//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// A is the requested and forwarded channel model, B remains the configured
// channel pricing model, and C is the actual upstream account model.
func TestChannelIntelligenceRecordUsageChargesConfiguredChannelPrice(t *testing.T) {
	groupID := int64(707)
	inputPrice, outputPrice := 3e-6, 15e-6
	channel := Channel{
		ID: 708, Status: StatusActive, GroupIDs: []int64{groupID},
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelMapping:       map[string]map[string]string{PlatformOpenAI: {"client-model-a": "priced-model-b"}},
		ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{"priced-model-b"}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
	}
	channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformOpenAI}))
	ctx := WithIntelligenceRouting(context.Background())
	mapping := channelSvc.ResolveChannelMapping(ctx, groupID, "client-model-a")
	require.False(t, mapping.Mapped)
	require.Equal(t, "client-model-a", mapping.MappedModel)
	fields := mapping.ToUsageFields("client-model-a", "account-model-c")
	require.Equal(t, "priced-model-b", fields.ChannelMappedModel)
	require.Equal(t, "client-model-a→account-model-c", fields.ModelMappingChain)

	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.channelService = channelSvc
	svc.resolver = NewModelPricingResolver(channelSvc, svc.billingService)
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{
			RequestID: "iq-channel-price", Model: "client-model-a", BillingModel: "account-model-c", UpstreamModel: "account-model-c",
			Usage: OpenAIUsage{InputTokens: 1200, OutputTokens: 300}, Duration: time.Second,
		},
		APIKey: &APIKey{ID: 710, GroupID: &groupID, Quota: 100, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}},
		User:   &User{ID: 711}, Account: &Account{ID: 712, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Extra: map[string]any{IntelligenceRoutingExtraKey: true}},
		APIKeyService: quotaSvc, ChannelUsageFields: fields,
	})
	require.NoError(t, err)
	want := QuantizeUsageBillingAmount(1200*inputPrice + 300*outputPrice)
	require.NotNil(t, usageRepo.lastLog)
	require.Equal(t, "client-model-a", usageRepo.lastLog.RequestedModel)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, "account-model-c", *usageRepo.lastLog.UpstreamModel)
	require.NotNil(t, usageRepo.lastLog.ModelMappingChain)
	require.Equal(t, "client-model-a→account-model-c", *usageRepo.lastLog.ModelMappingChain)
	require.InDelta(t, want, usageRepo.lastLog.ActualCost, 1e-12)
	require.Equal(t, 1, userRepo.deductCalls)
	require.InDelta(t, want, userRepo.lastAmount, 1e-12)
	require.Equal(t, 1, quotaSvc.quotaCalls)
	require.InDelta(t, want, quotaSvc.lastAmount, 1e-12)
}
