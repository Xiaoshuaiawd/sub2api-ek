//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAccountOriginalMappingTakesPriorityWithoutChainingTarget(t *testing.T) {
	const source, channel, target = "gpt-6.1-sol", "gpt-6-terra", "gpt-6-luna"
	for _, rules := range []map[string]any{
		{source: target, target: "wrong-second-hop"},
		{"gpt-6.1-*": target, target: "wrong-second-hop"},
	} {
		account := &Account{ID: 7, Platform: PlatformOpenAI, Credentials: map[string]any{"model_mapping": rules}}
		ctx := WithAccountModelMapping(context.Background(), source, channel)
		require.Equal(t, source, AccountModelMappingInput(ctx, account, channel, false))
		require.Equal(t, target, AccountMappedModelForRequest(ctx, account, channel))
		mapping := ChannelMappingResult{Mapped: true, MappedModel: channel, ChannelID: 8, BillingModelSource: BillingModelSourceChannelMapped}.ForAccount(account, source, false)
		fields := mapping.ToUsageFields(source, target)
		require.Equal(t, channel, fields.ChannelMappedModel)
		require.False(t, fields.ChannelMappingApplied)
		require.Equal(t, source+"→"+target, fields.ModelMappingChain)
	}
}

func TestAccountOriginalMappingIdentityAndLegacyFallback(t *testing.T) {
	const source, channel = "public-a", "channel-b"
	ctx := WithAccountModelMapping(context.Background(), source, channel)
	identity := &Account{ID: 1, Credentials: map[string]any{"model_mapping": map[string]any{source: source, channel: "wrong"}}}
	require.Equal(t, source, AccountMappedModelForRequest(ctx, identity, channel))
	fallback := &Account{ID: 2, Credentials: map[string]any{"model_mapping": map[string]any{channel: "account-c"}}}
	require.Equal(t, channel, AccountModelMappingInput(ctx, fallback, channel, false))
	require.Equal(t, "account-c", AccountMappedModelForRequest(ctx, fallback, channel))
	require.Equal(t, "wrong", AccountMappedModelForRequest(context.Background(), identity, channel), "no-context APIs retain the legacy channel-input contract")
}

func TestAccountOriginalMappingExcludesProviderDefaultsAndProjection(t *testing.T) {
	for _, platform := range []string{PlatformGrok, PlatformAntigravity} {
		account := &Account{Platform: platform}
		_, matched := account.ResolveConfiguredModelMapping("grok-4")
		require.False(t, matched)
	}
	source := &Account{Credentials: map[string]any{"model_mapping": map[string]any{"public-a": "configured-c"}}}
	view := &Account{gatewayProtocolSource: source, Credentials: map[string]any{"model_mapping": map[string]any{"public-a": "projected-b"}}}
	target, matched := view.ResolveConfiguredModelMapping("public-a")
	require.True(t, matched)
	require.Equal(t, "configured-c", target)
}

func TestAccountOriginalMappingResolvedMarkerDoesNotEscapeAccountOrTurn(t *testing.T) {
	ctx := WithAccountModelMapping(context.Background(), "a", "b")
	first := &Account{ID: 1, Credentials: map[string]any{"model_mapping": map[string]any{"a": "c", "c": "wrong"}}}
	ctx = WithResolvedAccountModel(ctx, first.ID, "c")
	require.Equal(t, "c", AccountMappedModelForRequest(ctx, first, "c"))
	second := &Account{ID: 2, Credentials: map[string]any{"model_mapping": map[string]any{"a": "second-c"}}}
	require.Equal(t, "second-c", AccountMappedModelForRequest(ctx, second, "c"))
	fallback := &Account{ID: 3, Credentials: map[string]any{"model_mapping": map[string]any{"b": "fallback-c", "c": "wrong-stale-hop"}}}
	require.Equal(t, "b", AccountModelMappingInput(ctx, fallback, "c", false))
	require.Equal(t, "fallback-c", AccountMappedModelForRequest(ctx, fallback, "c"))
	next := WithAccountModelMapping(ctx, "next-a", "next-b")
	_, resolved := ResolvedAccountModel(next, first, "c")
	require.False(t, resolved)
}

func TestAccountOriginalMappingKeepsPublicAPIKeyPricingWithoutChannelRule(t *testing.T) {
	account := &Account{Credentials: map[string]any{"model_mapping": map[string]any{"key-route-a": "account-c"}}}
	mapping := ChannelMappingResult{MappedModel: "key-route-a"}.ForAccount(account, "key-route-a", false)
	fields := mapping.ToUsageFields("public-client-p", "account-c")
	require.Equal(t, "public-client-p", fields.ChannelMappedModel, "a missing channel rule must not change the previous billing basis")
}

func TestAccountOriginalMappingKeepsConfiguredChannelPrice(t *testing.T) {
	const source, channelTarget, accountTarget = "gpt-6.1-sol", "gpt-6-terra", "gpt-6-luna"
	groupID := int64(707)
	inputPrice, outputPrice := 3e-6, 15e-6
	channel := Channel{
		ID: 708, Status: StatusActive, GroupIDs: []int64{groupID},
		BillingModelSource: BillingModelSourceChannelMapped,
		ModelMapping:       map[string]map[string]string{PlatformOpenAI: {source: channelTarget}},
		ModelPricing:       []ChannelModelPricing{{Platform: PlatformOpenAI, Models: []string{channelTarget}, BillingMode: BillingModeToken, InputPrice: &inputPrice, OutputPrice: &outputPrice}},
	}
	channelSvc := newTestChannelService(makeStandardRepo(channel, map[int64]string{groupID: PlatformOpenAI}))
	ctx := WithAccountModelMapping(context.Background(), source, channelTarget)
	account := &Account{ID: 712, Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{source: accountTarget}}}
	mapping := channelSvc.ResolveChannelMapping(ctx, groupID, source).ForAccount(account, source, false)
	fields := mapping.ToUsageFields(source, accountTarget)
	require.Equal(t, channelTarget, fields.ChannelMappedModel)
	require.Equal(t, source+"→"+accountTarget, fields.ModelMappingChain)
	usageRepo := &openAIRecordUsageLogRepoStub{inserted: true}
	userRepo := &openAIRecordUsageUserRepoStub{}
	quotaSvc := &openAIRecordUsageAPIKeyQuotaStub{}
	svc := newOpenAIRecordUsageServiceForTest(usageRepo, userRepo, &openAIRecordUsageSubRepoStub{}, nil)
	svc.channelService = channelSvc
	svc.resolver = NewModelPricingResolver(channelSvc, svc.billingService)
	err := svc.RecordUsage(ctx, &OpenAIRecordUsageInput{
		Result: &OpenAIForwardResult{RequestID: "account-priority-channel-price", Model: source, BillingModel: accountTarget, UpstreamModel: accountTarget,
			Usage: OpenAIUsage{InputTokens: 1200, OutputTokens: 300}, Duration: time.Second},
		APIKey: &APIKey{ID: 710, GroupID: &groupID, Quota: 100, Group: &Group{ID: groupID, Platform: PlatformOpenAI, RateMultiplier: 1}},
		User:   &User{ID: 711}, Account: account, APIKeyService: quotaSvc, ChannelUsageFields: fields,
	})
	require.NoError(t, err)
	want := QuantizeUsageBillingAmount(1200*inputPrice + 300*outputPrice)
	require.InDelta(t, want, usageRepo.lastLog.ActualCost, 1e-12)
	require.NotNil(t, usageRepo.lastLog.UpstreamModel)
	require.Equal(t, accountTarget, *usageRepo.lastLog.UpstreamModel)
	require.Equal(t, 1, userRepo.deductCalls)
}
