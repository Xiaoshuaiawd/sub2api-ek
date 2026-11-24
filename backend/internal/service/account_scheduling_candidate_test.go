//go:build unit

package service

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/xai"
	"github.com/stretchr/testify/require"
)

func TestSchedulingCandidateCopyKeepsCOWAndPublicIsolation(t *testing.T) {
	master := Account{ID: 1, Name: "master", Platform: PlatformAnthropic,
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet": "target"}},
		Extra:       map[string]any{modelRateLimitsKey: map[string]any{}},
		GroupIDs:    []int64{7}, AccountGroups: []AccountGroup{{GroupID: 7, AllowedModels: []string{"claude-sonnet"}}},
	}
	master.PrepareSharedSnapshot()
	first, second := master.CopySchedulingCandidate(), master.CopySchedulingCandidate()
	require.True(t, first.IsSchedulingCandidate())
	require.True(t, first.modelMappingCacheShared)
	first.Name = "request-local"
	setAccountModelRateLimitSnapshot(&first, "target", time.Now().Add(time.Hour), "local", time.Now())
	require.True(t, first.isModelRateLimitedWithContext(context.Background(), "claude-sonnet"))
	require.False(t, second.isModelRateLimitedWithContext(context.Background(), "claude-sonnet"))
	require.False(t, master.isModelRateLimitedWithContext(context.Background(), "claude-sonnet"))
	require.Equal(t, "master", second.Name)
	mutable := first.CopySchedulerSnapshot()
	require.False(t, mutable.IsSchedulingCandidate())
	mutable.Credentials["model_mapping"].(map[string]any)["claude-sonnet"] = "poison"
	mutable.Extra[modelRateLimitsKey].(map[string]any)["target"].(map[string]any)["reason"] = "poison"
	mutable.GroupIDs[0] = 99
	mutable.AccountGroups[0].AllowedModels[0] = "poison"
	require.Equal(t, "target", first.GetMappedModel("claude-sonnet"))
	require.Equal(t, "local", first.modelRateLimitReason("target"))
	require.Equal(t, int64(7), first.GroupIDs[0])
	require.Equal(t, "claude-sonnet", first.AccountGroups[0].AllowedModels[0])
	first.Credentials = map[string]any{"model_mapping": map[string]any{"claude-sonnet": "replacement"}}
	require.Equal(t, "replacement", first.GetMappedModel("claude-sonnet"))
	require.Equal(t, "target", second.GetMappedModel("claude-sonnet"))
	raw, err := json.Marshal(second)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "schedulingCandidate")
	require.NotContains(t, string(raw), "scheduling_candidate")
}

func TestSchedulingCandidateCopiesConcurrentMappingAndCOW(t *testing.T) {
	master := Account{Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet": "target"}}, Extra: map[string]any{}}
	master.PrepareSharedSnapshot()
	var wg sync.WaitGroup
	for worker := 0; worker < 16; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 20; i++ {
				candidate := master.CopySchedulingCandidate()
				if candidate.GetMappedModel("claude-sonnet") != "target" {
					t.Error("shared mapping changed")
				}
				setAccountModelRateLimitSnapshot(&candidate, "target", time.Now().Add(time.Hour), "own", time.Now())
				if !candidate.isModelRateLimitedWithContext(context.Background(), "claude-sonnet") {
					t.Error("own model limit was lost")
				}
			}
		}()
	}
	wg.Wait()
	require.Empty(t, master.Extra)
	require.Equal(t, "target", master.GetMappedModel("claude-sonnet"))
}

func TestSchedulingCandidateMappingVersionRebuildIsLocal(t *testing.T) {
	master := Account{Platform: PlatformAnthropic, Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet": "target"}}}
	master.PrepareSharedSnapshot()
	previous := master.modelMappingCacheRuntimeVersion
	xai.SetRuntimeModelMappingOptions(xai.RuntimeModelMappingOptions())
	candidate := master.CopySchedulingCandidate()
	require.True(t, candidate.SharedSnapshotCurrent())
	require.Equal(t, "target", candidate.GetMappedModel("claude-sonnet"))
	require.Equal(t, previous, master.modelMappingCacheRuntimeVersion, "rebuilding one candidate must not mutate the published master")
}

type schedulingCandidateHydrationCache struct {
	snapshotHydrationCache
	candidates                    []Account
	candidateReads, snapshotReads int
}

func (c *schedulingCandidateHydrationCache) GetSchedulingCandidates(context.Context, SchedulerBucket) ([]Account, bool, error) {
	c.candidateReads++
	return append([]Account(nil), c.candidates...), true, nil
}

func (c *schedulingCandidateHydrationCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	c.snapshotReads++
	return nil, false, nil
}

func TestOpenAISchedulingCandidatesHydrateSelectedAccount(t *testing.T) {
	for _, advanced := range []bool{false, true} {
		t.Run(map[bool]string{false: "legacy", true: "advanced"}[advanced], func(t *testing.T) {
			master := Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 1,
				Credentials: map[string]any{"model_mapping": map[string]any{"gpt-4": "gpt-4"}}}
			master.PrepareSharedSnapshot()
			full := master.CopySchedulerSnapshot()
			full.Credentials["api_key"] = "live-key"
			cache := &schedulingCandidateHydrationCache{candidates: []Account{master.CopySchedulingCandidate()}, snapshotHydrationCache: snapshotHydrationCache{accounts: map[int64]*Account{1: &full}}}
			svc := &OpenAIGatewayService{schedulerSnapshot: NewSchedulerSnapshotService(cache, nil, nil, nil, nil), cache: &stubGatewayCache{}}
			var selection *AccountSelectionResult
			var err error
			if advanced {
				scheduler := &defaultOpenAIAccountScheduler{service: svc, stats: newOpenAIAccountRuntimeStats()}
				selection, _, _, _, err = scheduler.selectByLoadBalance(context.Background(), OpenAIAccountScheduleRequest{Platform: PlatformOpenAI, RequestedModel: "gpt-4"})
			} else {
				selection, err = svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "gpt-4", nil)
			}
			require.NoError(t, err)
			require.NotNil(t, selection)
			require.False(t, selection.Account.IsSchedulingCandidate(), "read-only pool views must not escape into waits/forwarding")
			require.Equal(t, "live-key", selection.Account.GetOpenAIApiKey())
			require.Positive(t, cache.candidateReads)
			require.Zero(t, cache.snapshotReads)
			selection.Account.Credentials["model_mapping"].(map[string]any)["gpt-4"] = "local"
			require.Equal(t, "gpt-4", master.GetMappedModel("gpt-4"))
			if selection.ReleaseFunc != nil {
				selection.ReleaseFunc()
			}
		})
	}
}

func TestSchedulingCandidateFiltersOwnValuesWithoutChangingPublicInput(t *testing.T) {
	masters := []Account{{ID: 1, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}, {ID: 2, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}, {ID: 3, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true}}
	values := make([]Account, len(masters))
	for i := range masters {
		values[i] = masters[i].CopySchedulingCandidate()
	}
	performance := NewSchedulerSnapshotService(nil, nil, &performanceAccountRepoStub{paused: map[int64]bool{2: true}}, nil, nil)
	filtered, err := performance.filterPerformanceAccounts(context.Background(), values)
	require.NoError(t, err)
	require.Equal(t, []int64{1, 3}, []int64{filtered[0].ID, filtered[1].ID})
	require.Same(t, &values[0], &filtered[0])
	require.Zero(t, values[2].ID)
	require.Equal(t, int64(3), masters[2].ID)
	// Public mutable input retains the old non-mutating filter behavior.
	public := append([]Account(nil), masters...)
	publicFiltered, err := performance.filterPerformanceAccounts(context.Background(), public)
	require.NoError(t, err)
	require.NotSame(t, &public[0], &publicFiltered[0])
	require.Equal(t, int64(3), public[2].ID)
	// OpenAI threshold/intelligence filtering may also compact only its own
	// candidate values; replacing a scalar never edits the immutable master.
	masters[1].Extra = map[string]any{IntelligenceRoutingExtraKey: true}
	values = make([]Account, len(masters))
	for i := range masters {
		values[i] = masters[i].CopySchedulingCandidate()
	}
	openai := &OpenAIGatewayService{}
	thresholdFiltered := openai.filterOpenAIAccountsBySchedulingThreshold(context.Background(), values)
	require.Len(t, thresholdFiltered, 2)
	require.Same(t, &values[0], &thresholdFiltered[0])
	require.Equal(t, int64(3), thresholdFiltered[1].ID)
	require.Zero(t, values[2].ID)
	require.Equal(t, int64(2), masters[1].ID)
}
