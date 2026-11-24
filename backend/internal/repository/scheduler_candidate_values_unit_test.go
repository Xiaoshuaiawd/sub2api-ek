//go:build unit

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type schedulingCandidatesReader interface {
	GetSchedulingCandidates(context.Context, service.SchedulerBucket) ([]service.Account, bool, error)
}

func schedulingCandidates(t *testing.T, cache *schedulerCache, bucket service.SchedulerBucket) []service.Account {
	t.Helper()
	reader, ok := any(cache).(schedulingCandidatesReader)
	require.True(t, ok, "the scheduler must have an explicit candidate reader rather than weakening public snapshots")
	values, hit, err := reader.GetSchedulingCandidates(context.Background(), bucket)
	require.NoError(t, err)
	require.True(t, hit)
	return values
}

func TestSchedulerCacheSchedulingCandidatesKeepPublicSnapshotIsolation(t *testing.T) {
	cache, _, _ := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)
	bucket := memoTestBucket(901)
	account := memoTestAccount(901, "original")
	account.Extra = map[string]any{"model_rate_limits": map[string]any{"claude-sonnet": map[string]any{"rate_limit_reset_at": "2099-01-01T00:00:00Z"}}}
	memoPublish(t, cache, bucket, account)
	first := schedulingCandidates(t, cache, bucket)
	require.Len(t, first, 1)
	marker, ok := any(&first[0]).(interface{ IsSchedulingCandidate() bool })
	require.True(t, ok)
	require.True(t, marker.IsSchedulingCandidate())
	first[0].Name, first[0].Schedulable = "local", false
	second := schedulingCandidates(t, cache, bucket)
	require.Equal(t, "original", second[0].Name)
	require.True(t, second[0].Schedulable)
	public, hit, err := cache.GetSnapshotValues(context.Background(), bucket)
	require.NoError(t, err)
	require.True(t, hit)
	public[0].Credentials["model_mapping"].(map[string]any)["claude-sonnet"] = "poison"
	public[0].Extra["model_rate_limits"].(map[string]any)["claude-sonnet"].(map[string]any)["rate_limit_reset_at"] = "poison"
	third := schedulingCandidates(t, cache, bucket)
	require.Equal(t, "sonnet-original", third[0].GetMappedModel("claude-sonnet"))
	require.Equal(t, "2099-01-01T00:00:00Z", third[0].Extra["model_rate_limits"].(map[string]any)["claude-sonnet"].(map[string]any)["rate_limit_reset_at"])
}

func TestSchedulerCacheSchedulingCandidatesObserveRevisionLastUsedAndRevocation(t *testing.T) {
	ctx := context.Background()
	cache, _, _ := newSchedulerCacheMemoUnit(t, defaultSchedulerSnapshotWriteChunkSize)
	bucket := memoTestBucket(902)
	account := memoTestAccount(902, "old")
	memoPublish(t, cache, bucket, account)
	first := schedulingCandidates(t, cache, bucket)
	account.Name = "new"
	account.Credentials = map[string]any{"model_mapping": map[string]any{"claude-sonnet": "updated"}}
	require.NoError(t, cache.SetAccount(ctx, &account))
	latest := time.Now().UTC().Truncate(time.Millisecond)
	require.NoError(t, cache.UpdateLastUsed(ctx, map[int64]time.Time{account.ID: latest}))
	second := schedulingCandidates(t, cache, bucket)
	require.Equal(t, "new", second[0].Name)
	require.Equal(t, "updated", second[0].GetMappedModel("claude-sonnet"))
	require.Equal(t, latest, *second[0].LastUsedAt)
	require.Equal(t, "old", first[0].Name)
	require.NoError(t, cache.DeleteAccount(ctx, account.ID))
	reader := any(cache).(schedulingCandidatesReader)
	_, hit, err := reader.GetSchedulingCandidates(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit)
}

var schedulingCandidateCopySink []service.Account

func BenchmarkSchedulingCandidateCopies(b *testing.B) {
	for _, models := range []int{2, 64} {
		for _, size := range []int{100, 1_000, 10_000} {
			masters := schedulerCacheBenchmarkAccounts(size)
			mapping := make(map[string]any, models)
			for i := 0; i < models; i++ {
				mapping[fmt.Sprintf("model-%03d", i)] = fmt.Sprintf("target-%03d", i)
			}
			for i := range masters {
				masters[i].Credentials["model_mapping"] = mapping
				masters[i] = buildSchedulerMetadataAccount(masters[i])
				masters[i].PrepareSharedSnapshot()
			}
			for _, candidate := range []bool{false, true} {
				b.Run(fmt.Sprintf("models_%d/accounts_%d/candidate_%t", models, size, candidate), func(b *testing.B) {
					var copyCandidate func(*service.Account) service.Account
					if candidate {
						copyCandidate = (*service.Account).CopySchedulingCandidate
					} else {
						copyCandidate = (*service.Account).CopySchedulerSnapshot
					}
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						copies := make([]service.Account, len(masters))
						for j := range masters {
							copies[j] = copyCandidate(&masters[j])
						}
						schedulingCandidateCopySink = copies
					}
				})
			}
		}
	}
}
