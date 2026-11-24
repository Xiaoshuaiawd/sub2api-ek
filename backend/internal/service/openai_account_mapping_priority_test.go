//go:build unit

package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const schedulerMappingInput = "gpt-6.1-sol"
const schedulerChannelTarget = "gpt-6-terra"
const schedulerAccountTarget = "gpt-6-luna"

func mappingPriorityAccount(id int64, mapping map[string]any) Account {
	return Account{ID: id, Name: "mapping-priority", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1, Priority: 1,
		Credentials: map[string]any{"api_key": "synthetic", "model_mapping": mapping}}
}

func mappingPriorityScheduler(accounts []Account) (*OpenAIGatewayService, *schedulingCandidateHydrationCache) {
	cache := &schedulingCandidateHydrationCache{snapshotHydrationCache: snapshotHydrationCache{accounts: make(map[int64]*Account)}}
	for i := range accounts {
		accounts[i].PrepareSharedSnapshot()
		cache.candidates = append(cache.candidates, accounts[i].CopySchedulingCandidate())
		full := accounts[i].CopySchedulerSnapshot()
		cache.accounts[accounts[i].ID] = &full
	}
	return &OpenAIGatewayService{schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil), cache: &stubGatewayCache{}}, cache
}

func selectMappingPriority(t *testing.T, svc *OpenAIGatewayService, ctx context.Context, advanced bool, excluded map[int64]struct{}) (*AccountSelectionResult, error) {
	t.Helper()
	if advanced {
		scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
		selection, _, _, _, err := scheduler.selectByLoadBalance(ctx, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: schedulerChannelTarget, ExcludedIDs: excluded})
		return selection, err
	}
	return svc.SelectAccountWithLoadAwareness(ctx, nil, "", schedulerChannelTarget, excluded)
}

func TestOpenAIAccountMappingPrioritySelectsSourceOnlyWhitelist(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		for _, rule := range []struct{ name, source, target string }{
			{"exact", schedulerMappingInput, schedulerAccountTarget},
			{"wildcard", "gpt-6.1-*", schedulerAccountTarget},
			{"identity", schedulerMappingInput, schedulerMappingInput},
		} {
			t.Run(rule.name+map[bool]string{false: "/legacy", true: "/advanced"}[advanced], func(t *testing.T) {
				svc, cache := mappingPriorityScheduler([]Account{mappingPriorityAccount(1, map[string]any{rule.source: rule.target})})
				before, err := json.Marshal(cache.candidates)
				require.NoError(t, err)
				ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
				ctx = WithOpenAIForwardModel(ctx, schedulerChannelTarget, false)
				selection, err := selectMappingPriority(t, svc, ctx, advanced, nil)
				require.NoError(t, err, "an explicit source rule must not be rejected by the channel target whitelist")
				require.NotNil(t, selection)
				require.Equal(t, int64(1), selection.Account.ID)
				require.False(t, selection.Account.IsSchedulingCandidate())
				require.Equal(t, rule.target, AccountMappedModelForRequest(ctx, selection.Account, schedulerChannelTarget))
				after, err := json.Marshal(cache.candidates)
				require.NoError(t, err)
				require.JSONEq(t, string(before), string(after), "candidate credentials and configuration must remain unchanged")
				if selection.ReleaseFunc != nil {
					selection.ReleaseFunc()
				}
			})
		}
	}
}

func TestOpenAIAccountMappingPriorityCooldownUsesAccountTarget(t *testing.T) {
	for _, blocked := range []struct{ model, reason string }{
		{schedulerAccountTarget, "model_rate_limited"},
		{schedulerChannelTarget, ""},
	} {
		t.Run(blocked.model, func(t *testing.T) {
			account := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget, schedulerChannelTarget: schedulerChannelTarget})
			setAccountModelRateLimitSnapshot(&account, blocked.model, time.Now().Add(time.Hour), "test", time.Now())
			ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
			reason := openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, &account, nil, PlatformOpenAI, schedulerChannelTarget, false, OpenAIEndpointCapabilityResponses)
			require.Equal(t, blocked.reason, reason)
		})
	}
}

func TestOpenAIAccountMappingPriorityDoesNotReusePreviousAccountDecision(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			other := "gpt-6-nova"
			svc, _ := mappingPriorityScheduler([]Account{mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget}), mappingPriorityAccount(2, map[string]any{schedulerMappingInput: other})})
			ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
			ctx = WithResolvedAccountModel(ctx, 1, schedulerAccountTarget)
			selection, err := selectMappingPriority(t, svc, ctx, advanced, map[int64]struct{}{1: {}})
			require.NoError(t, err)
			require.Equal(t, int64(2), selection.Account.ID)
			require.Equal(t, other, AccountMappedModelForRequest(ctx, selection.Account, schedulerChannelTarget))
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestOpenAIAccountMappingPriorityFreshDBMappingIsAuthoritative(t *testing.T) {
	for _, sameTimestamp := range []bool{false, true} {
		t.Run(map[bool]string{false: "newer primary", true: "same timestamp"}[sameTimestamp], func(t *testing.T) {
			snapshot := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget})
			fresh := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: "gpt-6-nova"})
			snapshot.UpdatedAt = time.Now().Add(-time.Minute)
			fresh.UpdatedAt = snapshot.UpdatedAt
			if !sameTimestamp {
				fresh.UpdatedAt = fresh.UpdatedAt.Add(time.Second)
			}
			svc, _ := mappingPriorityScheduler([]Account{snapshot})
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}}
			ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
			selection, err := selectMappingPriority(t, svc, ctx, false, nil)
			require.NoError(t, err)
			require.Equal(t, "gpt-6-nova", AccountMappedModelForRequest(ctx, selection.Account, schedulerChannelTarget))
			require.True(t, selection.Account.schedulingPrimaryRead)
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
			mutable := selection.Account.CopySchedulerSnapshot()
			candidate := selection.Account.CopySchedulingCandidate()
			require.False(t, mutable.schedulingPrimaryRead)
			require.False(t, candidate.schedulingPrimaryRead)
			// Neither a prior primary mark nor a wire mark bypasses the next
			// authoritative recheck when the account's whitelist is revoked.
			fresh.Credentials["model_mapping"] = map[string]any{"unrelated": "unrelated"}
			svc.accountRepo = schedulerTestOpenAIAccountRepo{accounts: []Account{fresh}}
			ctx = WithResolvedAccountModel(ctx, fresh.ID, schedulerAccountTarget)
			require.Nil(t, svc.recheckSelectedOpenAIAccountFromDB(ctx, selection.Account, nil, PlatformOpenAI, schedulerChannelTarget, false, OpenAIEndpointCapabilityResponses))
			require.Equal(t, "model_not_supported", openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, &fresh, nil, PlatformOpenAI, schedulerChannelTarget, false, OpenAIEndpointCapabilityResponses))
		})
	}
}

func TestOpenAIAccountMappingPriorityContextResolversDoNotMapTargetTwice(t *testing.T) {
	account := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget, schedulerAccountTarget: "wrong-second-target", schedulerChannelTarget: "legacy-channel-target"})
	ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
	require.Equal(t, schedulerAccountTarget, ResolveOpenAIAccountUpstreamModelForRequestWithContext(ctx, &account, schedulerChannelTarget, false))
	billing, wire := resolveOpenAIForwardMappedModelsForRequest(ctx, &account, schedulerChannelTarget, false)
	require.Equal(t, schedulerAccountTarget, billing)
	require.Equal(t, schedulerAccountTarget, wire)
	account.Extra = map[string]any{"openai_passthrough": true}
	require.Equal(t, schedulerAccountTarget, ResolveOpenAIAccountUpstreamModelForRequestWithContext(ctx, &account, schedulerChannelTarget, false), "an explicit original account rule also wins passthrough")
	require.Equal(t, schedulerChannelTarget, ResolveOpenAIAccountUpstreamModelForRequestWithContext(context.Background(), &account, schedulerChannelTarget, false), "no-context passthrough retains its legacy contract")
	account.Credentials["compact_model_mapping"] = map[string]any{schedulerMappingInput: "compact-original"}
	billing, wire = resolveOpenAIForwardMappedModelsForRequest(ctx, &account, schedulerChannelTarget, true)
	require.Equal(t, schedulerAccountTarget, billing)
	require.Equal(t, "compact-original", wire)
	ctx = WithResolvedAccountModel(ctx, account.ID, schedulerAccountTarget)
	require.Equal(t, schedulerAccountTarget, ResolveOpenAIAccountUpstreamModelForRequestWithContext(ctx, &account, schedulerAccountTarget, false))
	fresh := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: "fresh-target"})
	require.Equal(t, "fresh-target", resolveOpenAIAccountModelForScheduling(ctx, &fresh, schedulerAccountTarget, false), "fresh authority must ignore the earlier serialized target")
}

func TestOpenAIAccountMappingPriorityCannotEscapeInitialTargetScope(t *testing.T) {
	account := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget, schedulerChannelTarget: schedulerChannelTarget})
	account.Type = AccountTypeOAuth
	account.Credentials[OAuthInitialModelSelectionKey] = []string{schedulerMappingInput, schedulerChannelTarget}
	ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
	ctx = WithResolvedAccountModel(ctx, account.ID, schedulerAccountTarget)
	require.Equal(t, schedulerAccountTarget, AccountMappedModelForRequest(ctx, &account, schedulerChannelTarget), "priority detection uses the raw rule even when authorization denies its target")
	reason := openAICompatibleAccountEligibilityFailureReasonBeforeProfit(ctx, &account, nil, PlatformOpenAI, schedulerChannelTarget, false, OpenAIEndpointCapabilityResponses)
	require.Equal(t, "model_not_supported", reason)
	svc := &OpenAIGatewayService{}
	scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
	allowed, reason := scheduler.isAccountRequestCompatibleReason(ctx, &account, OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: schedulerChannelTarget})
	require.False(t, allowed)
	require.Equal(t, "model_not_supported", reason)
	_, err := svc.admitOpenAITurnWithGroup(ctx, &account, schedulerAccountTarget, 0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_not_supported")
}

func TestOpenAIAccountMappingPriorityUpstreamReservationPredictsActualAccountModel(t *testing.T) {
	ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
	account := mappingPriorityAccount(1, map[string]any{schedulerMappingInput: schedulerAccountTarget, schedulerAccountTarget: "wrong-second-target", schedulerChannelTarget: schedulerChannelTarget})
	require.Equal(t, []string{schedulerAccountTarget}, accountMappedModelsFromContext(ctx, []Account{account}, schedulerChannelTarget))
	groupID := int64(1)
	deps := inflightEstimateDeps{resolveMapping: func(context.Context, int64, string) ChannelMappingResult {
		return ChannelMappingResult{Mapped: true, MappedModel: schedulerChannelTarget, BillingModelSource: BillingModelSourceUpstream}
	}}
	_, _, input, probe := inflightBillingModelCandidates(ctx, deps, &APIKey{GroupID: &groupID}, schedulerMappingInput)
	require.Equal(t, schedulerChannelTarget, input)
	require.True(t, probe, "upstream/response pricing must reserve the possible actual account target")
}

func TestOpenAIAccountMappingPriorityAdmissionUsesChannelWhenBodyStillHasOriginal(t *testing.T) {
	account := mappingPriorityAccount(1, map[string]any{schedulerChannelTarget: schedulerChannelTarget})
	ctx := WithAccountModelMapping(context.Background(), schedulerMappingInput, schedulerChannelTarget)
	svc := &OpenAIGatewayService{}
	latest, err := svc.admitOpenAITurnWithGroup(ctx, &account, schedulerMappingInput, 0, false)
	require.NoError(t, err, "pre-mapping adapters must authorize the preserved B fallback when no A rule exists")
	require.Equal(t, account.ID, latest.ID)
	// Recovering the channel fallback must still reject an account that no
	// longer owns B, rather than falling back to an unrelated raw source.
	account.Credentials["model_mapping"] = map[string]any{"unrelated": "unrelated"}
	_, err = svc.admitOpenAITurnWithGroup(ctx, &account, schedulerMappingInput, 0, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "model_not_supported")
}
