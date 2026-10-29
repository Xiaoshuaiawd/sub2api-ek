//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceRoutingGatewaySelection(t *testing.T) {
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Priority: 1, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformAnthropic, Priority: 2, Status: StatusActive, Schedulable: true, Extra: map[string]any{"intelligence_routing_enabled": true}},
	}
	repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{}}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	cache := &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}}
	svc := &GatewayService{accountRepo: repo, cache: cache, cfg: testConfig()}
	normal, err := svc.selectAccountForModelWithPlatform(context.Background(), nil, "", "claude-3-5-sonnet-20241022", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(1), normal.ID)
	ctx := WithIntelligenceRouting(context.Background())
	selected, err := svc.selectAccountForModelWithPlatform(ctx, nil, "", "claude-3-5-sonnet-20241022", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(2), selected.ID)
	sticky, err := svc.selectAccountForModelWithPlatform(ctx, nil, "sticky", "claude-3-5-sonnet-20241022", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(2), sticky.ID)
	_, err = svc.selectAccountForModelWithPlatform(ctx, nil, "", "claude-3-5-sonnet-20241022", map[int64]struct{}{2: {}}, PlatformAnthropic)
	require.Error(t, err)
}

func TestIntelligenceRoutingPreviousResponseDoesNotUseIneligibleAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(23)
	account := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}, cache: cache, cfg: newOpenAIWSV2TestConfig(), openaiWSStateStore: store}
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_iq", account.ID, time.Hour))
	selection, err := svc.SelectAccountByPreviousResponseID(WithIntelligenceRouting(ctx), &groupID, "resp_iq", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection)
	bound, err := store.GetResponseAccount(ctx, groupID, "resp_iq")
	require.NoError(t, err)
	require.Equal(t, account.ID, bound)
}

func TestIntelligenceRoutingOpenAISchedulers(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			resetOpenAIAdvancedSchedulerSettingCacheForTest()
			groupID := int64(55)
			accounts := []Account{
				{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true, Concurrency: 1},
				{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Priority: 2, Status: StatusActive, Schedulable: true, Concurrency: 1, Extra: map[string]any{IntelligenceRoutingExtraKey: true}},
			}
			svc := &OpenAIGatewayService{accountRepo: schedulerTestOpenAIAccountRepo{accounts: accounts}, cache: &schedulerTestGatewayCache{}, cfg: newSchedulerTestOpenAIWSV2Config(), concurrencyService: NewConcurrencyService(schedulerTestConcurrencyCache{})}
			if advanced {
				svc.rateLimitService = newOpenAIAdvancedSchedulerRateLimitService("true")
			}
			ordinary, _, err := svc.SelectAccountWithScheduler(context.Background(), &groupID, "", "", "gpt-5.1", map[int64]struct{}{1: {}}, OpenAIUpstreamTransportAny, false)
			require.Error(t, err)
			require.Nil(t, ordinary)
			ctx := WithIntelligenceRouting(context.Background())
			selection, _, err := svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", nil, OpenAIUpstreamTransportAny, false)
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			selection, _, err = svc.SelectAccountWithScheduler(ctx, &groupID, "", "", "gpt-5.1", map[int64]struct{}{2: {}}, OpenAIUpstreamTransportAny, false)
			require.Error(t, err)
			require.Nil(t, selection)
		})
	}
}

func TestIntelligenceRoutingTurnAdmissionUsesLatestAccount(t *testing.T) {
	selected := ticketTestAccount(987)
	selected.Extra = map[string]any{IntelligenceRoutingExtraKey: true}
	latest := *selected
	latest.Extra = map[string]any{IntelligenceRoutingExtraKey: false}
	svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: &latest}}
	_, err := svc.AdmitOpenAITurn(WithIntelligenceRouting(context.Background()), nil, selected, "gpt-5.5")
	require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
	_, err = svc.AdmitOpenAITurn(context.Background(), nil, selected, "gpt-5.5")
	require.NoError(t, err)
}

func TestIntelligenceRoutingPostSlotCheckWithoutProfitGate(t *testing.T) {
	account := &Account{ID: 1}
	_, vetoed, reason := profitControlVetoLatest(WithIntelligenceRouting(context.Background()), account, nil)
	require.True(t, vetoed)
	require.Equal(t, "intelligence_routing_class_mismatch", reason)
	_, vetoed, _ = profitControlVetoLatest(context.Background(), account, nil)
	require.False(t, vetoed)
}

func TestIntelligenceRoutingOrdinaryRequestExcludesDedicatedAccounts(t *testing.T) {
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic, Priority: 1, Status: StatusActive, Schedulable: true, Extra: map[string]any{IntelligenceRoutingExtraKey: true}},
		{ID: 2, Platform: PlatformAnthropic, Priority: 2, Status: StatusActive, Schedulable: true},
	}
	repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: map[int64]*Account{}}
	for i := range repo.accounts {
		repo.accountsByID[repo.accounts[i].ID] = &repo.accounts[i]
	}
	cache := &mockGatewayCacheForPlatform{sessionBindings: map[string]int64{"sticky": 1}}
	svc := &GatewayService{accountRepo: repo, cache: cache, cfg: testConfig()}
	selected, err := svc.selectAccountForModelWithPlatform(context.Background(), nil, "sticky", "claude-3-5-sonnet-20241022", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(2), selected.ID)
	_, err = svc.selectAccountForModelWithPlatform(context.Background(), nil, "", "claude-3-5-sonnet-20241022", map[int64]struct{}{2: {}}, PlatformAnthropic)
	require.Error(t, err)
}

func TestIntelligenceRoutingOrdinaryTurnRejectsNewlyDedicatedAccount(t *testing.T) {
	selected := ticketTestAccount(988)
	selected.Extra = nil
	latest := *selected
	latest.Extra = map[string]any{IntelligenceRoutingExtraKey: true}
	svc := &OpenAIGatewayService{accountRepo: &turnAdmissionRepo{account: &latest}}
	_, err := svc.AdmitOpenAITurn(context.Background(), nil, selected, "gpt-5.5")
	require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
	_, vetoed, _ := profitControlVetoLatest(context.Background(), &latest, nil)
	require.True(t, vetoed)
}

func TestIntelligenceRoutingOrdinaryPreviousResponseExcludesDedicatedAccount(t *testing.T) {
	ctx := context.Background()
	groupID := int64(24)
	account := Account{ID: 2, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 2, Extra: map[string]any{IntelligenceRoutingExtraKey: true}}
	cache := &stubGatewayCache{}
	store := NewOpenAIWSStateStore(cache)
	svc := &OpenAIGatewayService{accountRepo: stubOpenAIAccountRepo{accounts: []Account{account}}, cache: cache, cfg: newOpenAIWSV2TestConfig(), openaiWSStateStore: store}
	require.NoError(t, store.BindResponseAccount(ctx, groupID, "resp_iq_ordinary", account.ID, time.Hour))
	selection, err := svc.SelectAccountByPreviousResponseID(ctx, &groupID, "resp_iq_ordinary", "gpt-5.1", nil, false)
	require.NoError(t, err)
	require.Nil(t, selection)
}

func TestIntelligenceRoutingGeminiModelsEndpointSkipsDedicatedAccount(t *testing.T) {
	groupID := int64(8)
	dedicated := Account{ID: 1, Platform: PlatformGemini, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "k", "model_mapping": map[string]any{"test-only": "test-only"}}, Extra: map[string]any{IntelligenceRoutingExtraKey: true}}
	ordinary := Account{ID: 2, Platform: PlatformGemini, Type: AccountTypeOAuth, Credentials: map[string]any{"model_mapping": map[string]any{"ordinary": "ordinary"}}}
	svc := newGeminiModelsEndpointsTestService([]Account{dedicated, ordinary}, nil)
	selected, err := svc.SelectAccountForModelsEndpoints(context.Background(), &groupID)
	require.NoError(t, err)
	require.Equal(t, ordinary.ID, selected.ID)
	models, err := svc.DeclaredModelIDs(context.Background(), &groupID)
	require.NoError(t, err)
	require.Equal(t, []string{"ordinary"}, models)
}

func TestIntelligenceRoutingRequiredOverridesConnectionContext(t *testing.T) {
	ctx := WithIntelligenceRouting(context.Background())
	require.True(t, IsIntelligenceRouting(ctx))
	ctx = WithIntelligenceRoutingRequired(ctx, false)
	require.False(t, IsIntelligenceRouting(ctx))
	require.True(t, IsIntelligenceRouting(WithIntelligenceRoutingRequired(ctx, true)))
}

func TestIntelligenceRoutingPostSlotCheckReloadsWithoutSnapshot(t *testing.T) {
	selected := Account{ID: 123, Platform: PlatformAnthropic}
	latest := selected
	latest.Extra = map[string]any{IntelligenceRoutingExtraKey: true}
	repo := &mockAccountRepoForPlatform{accountsByID: map[int64]*Account{123: &latest}}
	svc := &GatewayService{accountRepo: repo}
	_, vetoed, _ := svc.GatewayProfitControlVetoLatest(context.Background(), &selected)
	require.True(t, vetoed)
}
