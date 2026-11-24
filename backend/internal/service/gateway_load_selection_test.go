//go:build unit

package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/stretchr/testify/require"
)

type gatewayLayer2LoadCache struct {
	*mockConcurrencyCache
	attempts []int64
	released map[int64]int
	acquire  func(context.Context, int64) (bool, error)
}

func (c *gatewayLayer2LoadCache) AcquireAccountSlot(ctx context.Context, id int64, _ int, _ string) (bool, error) {
	c.attempts = append(c.attempts, id)
	if c.acquire != nil {
		return c.acquire(ctx, id)
	}
	return false, nil
}

func (c *gatewayLayer2LoadCache) ReleaseAccountSlot(_ context.Context, id int64, _ string) error {
	c.released[id]++
	return nil
}

func newGatewayLayer2Fixture(accounts []Account) (*GatewayService, *gatewayLayer2LoadCache) {
	repo := &mockAccountRepoForPlatform{accounts: accounts, accountsByID: make(map[int64]*Account, len(accounts))}
	for i := range accounts {
		repo.accountsByID[accounts[i].ID] = &accounts[i]
	}
	cache := &gatewayLayer2LoadCache{mockConcurrencyCache: &mockConcurrencyCache{}, released: make(map[int64]int)}
	cfg := testConfig()
	cfg.Gateway.Scheduling.LoadBatchEnabled = true
	return &GatewayService{accountRepo: repo, cache: &mockGatewayCacheForPlatform{}, cfg: cfg, concurrencyService: NewConcurrencyService(cache)}, cache
}

func gatewayLayer2Accounts(n int) []Account {
	accounts := make([]Account, n)
	for i := range accounts {
		accounts[i] = Account{ID: int64(i + 1), Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, Concurrency: 10}
	}
	return accounts
}

func TestGatewayLayer2AllocationBound(t *testing.T) {
	const n = 2048
	svc, cache := newGatewayLayer2Fixture(gatewayLayer2Accounts(n))
	cache.attempts = make([]int64, 0, n)
	result := testing.Benchmark(func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			cache.attempts = cache.attempts[:0]
			selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "", nil, "", 0)
			if err != nil || selection == nil || selection.Acquired || len(cache.attempts) != n {
				b.Fatalf("selection=%+v err=%v attempts=%d", selection, err, len(cache.attempts))
			}
		}
	})
	t.Logf("all_slots_busy accounts=%d bytes/op=%d allocs/op=%d ns/op=%d", n, result.AllocedBytesPerOp(), result.AllocsPerOp(), result.NsPerOp())
	// Includes the real gateway's candidate filtering, load query and fallback.
	// A linear allowance leaves room for Account snapshots, but excludes the
	// previous sum of N full-sized filter/removal buffers.
	require.Less(t, result.AllocedBytesPerOp(), int64(n*8192))
	seen := make(map[int64]bool, n)
	for _, id := range cache.attempts {
		require.False(t, seen[id], "a candidate was attempted twice")
		seen[id] = true
	}
}

// This deliberately uses the existing layered filters as a small-input oracle,
// rather than duplicating the optimized queue's representation or comparator.
func gatewayLayer2LegacyEligible(remaining []accountWithLoad, reset, oauth bool) map[int64]bool {
	candidates := filterByMinPriority(remaining)
	if reset {
		candidates = filterBySoonestReset(candidates)
	}
	candidates = filterByMinLoadRate(candidates)
	selected := selectByLRU(candidates, oauth)
	eligible := make(map[int64]bool)
	for _, candidate := range candidates {
		a, b := candidate.account.LastUsedAt, selected.account.LastUsedAt
		sameUsed := a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
		if sameUsed && (!oauth || selected.account.Type != AccountTypeOAuth || candidate.account.Type == AccountTypeOAuth) {
			eligible[candidate.account.ID] = true
		}
	}
	return eligible
}

func TestGatewayLayer2RetriesMatchLegacyLayers(t *testing.T) {
	for _, reset := range []bool{false, true} {
		for _, oauth := range []bool{false, true} {
			t.Run(fmt.Sprintf("reset=%t/oauth=%t", reset, oauth), func(t *testing.T) {
				accounts := gatewayLayer2Accounts(36)
				base := time.Now().Add(-time.Hour)
				future := time.Now().Add(time.Hour)
				remaining := make([]accountWithLoad, 0, len(accounts))
				loads := make(map[int64]*AccountLoadInfo, len(accounts))
				for i := range accounts {
					account := &accounts[i]
					account.Priority = i % 3
					if oauth {
						account.Platform = PlatformGemini
					}
					if i%2 == 0 {
						account.Type = AccountTypeOAuth
					}
					if i%4 != 0 {
						lastUsed := base.Add(time.Duration(i%3) * time.Minute)
						account.LastUsedAt = &lastUsed
					}
					if i%3 != 0 {
						end := future.Add(time.Duration(i%4) * time.Minute)
						account.SessionWindowEnd = &end
					}
					if i%5 == 0 {
						factor := 20
						account.LoadFactor = &factor
					}
					load := &AccountLoadInfo{AccountID: account.ID, LoadRate: (i % 4) * 20}
					loads[account.ID] = load
					remaining = append(remaining, accountWithLoad{account: account, loadInfo: load})
				}
				svc, cache := newGatewayLayer2Fixture(accounts)
				svc.cfg.Gateway.Scheduling.PreferSoonestReset = reset
				cache.loadMap = loads
				cache.acquire = func(_ context.Context, id int64) (bool, error) {
					require.True(t, gatewayLayer2LegacyEligible(remaining, reset, oauth)[id], "candidate %d violated a legacy selection layer", id)
					for i, candidate := range remaining {
						if candidate.account.ID == id {
							remaining = append(remaining[:i], remaining[i+1:]...)
							break
						}
					}
					return false, nil
				}
				ctx := context.Background()
				if oauth {
					ctx = context.WithValue(ctx, ctxkey.ForcePlatform, PlatformGemini)
				}
				selection, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", "", nil, "", 0)
				require.NoError(t, err)
				require.False(t, selection.Acquired)
				require.Empty(t, remaining)
				require.Len(t, cache.attempts, len(accounts))
			})
		}
	}
}

func TestGatewayLayer2CancellationStopsRetries(t *testing.T) {
	svc, cache := newGatewayLayer2Fixture(gatewayLayer2Accounts(256))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache.acquire = func(context.Context, int64) (bool, error) {
		cancel()
		return false, ctx.Err()
	}
	selection, err := svc.SelectAccountWithLoadAwareness(ctx, nil, "", "", nil, "", 0)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, selection)
	require.Len(t, cache.attempts, 1)
}

type gatewayLayer2SessionCache struct{ SessionLimitCache }

func (gatewayLayer2SessionCache) RegisterSession(_ context.Context, id int64, _ string, _ int, _ time.Duration) (bool, error) {
	return id != 1, nil
}

func TestGatewayLayer2SessionDenialReleasesSlot(t *testing.T) {
	accounts := gatewayLayer2Accounts(2)
	accounts[0].Type, accounts[1].Type = AccountTypeOAuth, AccountTypeOAuth
	accounts[0].Priority, accounts[1].Priority = 0, 1
	accounts[0].Extra = map[string]any{"max_sessions": 1}
	accounts[1].Extra = map[string]any{"max_sessions": 1}
	svc, cache := newGatewayLayer2Fixture(accounts)
	svc.sessionLimitCache = gatewayLayer2SessionCache{}
	cache.acquire = func(context.Context, int64) (bool, error) { return true, nil }
	selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "new-session", "", nil, "", 0)
	require.NoError(t, err)
	require.True(t, selection.Acquired)
	require.Equal(t, int64(2), selection.Account.ID)
	require.Equal(t, []int64{1, 2}, cache.attempts)
	require.Equal(t, 1, cache.released[1])
	require.Zero(t, cache.released[2])
	selection.ReleaseFunc()
	require.Equal(t, 1, cache.released[2])
}

func BenchmarkGatewayLayer2AllSlotsBusy(b *testing.B) {
	for _, n := range []int{1024, 4096, 16384} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			svc, cache := newGatewayLayer2Fixture(gatewayLayer2Accounts(n))
			cache.attempts = make([]int64, 0, n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				cache.attempts = cache.attempts[:0]
				selection, err := svc.SelectAccountWithLoadAwareness(context.Background(), nil, "", "", nil, "", 0)
				if err != nil || selection == nil || len(cache.attempts) != n {
					b.Fatalf("selection=%+v err=%v attempts=%d", selection, err, len(cache.attempts))
				}
			}
		})
	}
}

func TestGatewayLayer2QueueWindowExpiryPreservesPriorityLayers(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	early, later, expired := now.Add(time.Minute), now.Add(2*time.Minute), now.Add(-time.Minute)
	accounts := gatewayLayer2Accounts(6)
	accounts[0].SessionWindowEnd, accounts[1].SessionWindowEnd = &early, &early
	accounts[2].SessionWindowEnd = &later
	accounts[4].SessionWindowEnd = &expired
	accounts[5].SessionWindowEnd, accounts[5].Priority = &early, 1
	loads := []int{0, 40, 80, 10, 20, 0}
	available := make([]accountWithLoad, len(accounts))
	for i := range accounts {
		available[i] = accountWithLoad{account: &accounts[i], loadInfo: &AccountLoadInfo{LoadRate: loads[i]}}
	}
	queue := newLoadAwareSelectionQueue(available, true, false)
	want := []int64{1, 3, 4, 5, 2, 6}
	for i, id := range want {
		at := now
		if i == 1 {
			at = early.Add(time.Second)
		} else if i > 1 {
			at = later.Add(time.Second)
		}
		selected, ok := queue.next(at)
		require.True(t, ok)
		require.Equal(t, id, selected.account.ID)
	}
	_, ok := queue.next(later.Add(time.Second))
	require.False(t, ok)
}

func TestGatewayLayer2ExpiredTieMergeRemainsUniform(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	end := now.Add(time.Second)
	accounts := gatewayLayer2Accounts(3)
	accounts[0].SessionWindowEnd, accounts[1].SessionWindowEnd = &end, &end
	const trials = 4096
	unknownChosen := 0
	for i := 0; i < trials; i++ {
		available := make([]accountWithLoad, len(accounts))
		for j := range accounts {
			available[j] = accountWithLoad{account: &accounts[j], loadInfo: &AccountLoadInfo{}}
		}
		queue := newLoadAwareSelectionQueue(available, true, false)
		first, ok := queue.next(now)
		require.True(t, ok)
		require.NotEqual(t, int64(3), first.account.ID)
		second, ok := queue.next(end.Add(time.Second))
		require.True(t, ok)
		if second.account.ID == 3 {
			unknownChosen++
		}
	}
	// A preassigned random rank biases the newly merged unknown account to
	// 2/3 after the first active candidate was consumed. Legacy selection draws
	// afresh over the two remaining equals, so its probability remains 1/2.
	require.InDelta(t, float64(trials)/2, unknownChosen, float64(trials)*0.06)
}

func TestGatewayLayer2QueueAttemptsDuplicateIdentityOnce(t *testing.T) {
	accounts := gatewayLayer2Accounts(3)
	accounts[2].ID, accounts[2].Priority = accounts[0].ID, 1
	available := make([]accountWithLoad, len(accounts))
	for i := range accounts {
		available[i] = accountWithLoad{account: &accounts[i], loadInfo: &AccountLoadInfo{}}
	}
	queue := newLoadAwareSelectionQueue(available, true, false)
	seen := map[int64]bool{}
	for {
		candidate, ok := queue.next(time.Now())
		if !ok {
			break
		}
		require.False(t, seen[candidate.account.ID])
		seen[candidate.account.ID] = true
	}
	require.Len(t, seen, 2)
}

func TestGatewayLayer2QueuePreservesSubsecondLRUBeforeOAuth(t *testing.T) {
	now := time.Date(2026, 11, 24, 0, 0, 0, 0, time.UTC)
	earlier, later := now.Add(100*time.Nanosecond), now.Add(900*time.Nanosecond)
	end := now.Add(time.Hour)
	for _, preferOAuth := range []bool{false, true} {
		for _, earlierOAuth := range []bool{false, true} {
			for _, window := range []string{"disabled", "active", "expired_merge"} {
				t.Run(fmt.Sprintf("prefer_oauth=%t/earlier_oauth=%t/%s", preferOAuth, earlierOAuth, window), func(t *testing.T) {
					accounts := gatewayLayer2Accounts(2)
					accounts[0].LastUsedAt, accounts[1].LastUsedAt = &earlier, &later
					accounts[1].Type = AccountTypeOAuth
					if earlierOAuth {
						accounts[0].Type, accounts[1].Type = AccountTypeOAuth, AccountTypeAPIKey
					}
					if window != "disabled" {
						accounts[0].SessionWindowEnd = &end
						if window == "active" {
							accounts[1].SessionWindowEnd = &end
						}
					}
					at := now
					if window == "expired_merge" {
						at = end.Add(time.Second)
					}
					for trial := 0; trial < 16; trial++ {
						available := []accountWithLoad{
							{account: &accounts[0], loadInfo: &AccountLoadInfo{}},
							{account: &accounts[1], loadInfo: &AccountLoadInfo{}},
						}
						// The old Layer2 LRU filter compares complete timestamps;
						// fallback's sameLastUsedAt groups by seconds and is not
						// an equivalent oracle for this selection path.
						want := selectByLRU(available, preferOAuth).account.ID
						queue := newLoadAwareSelectionQueue(available, window != "disabled", preferOAuth)
						selected, ok := queue.next(at)
						require.True(t, ok)
						require.Equal(t, int64(1), want)
						require.Equal(t, want, selected.account.ID)
					}
				})
			}
		}
	}
}

func TestGatewayLayer2QueueMatchesLegacyNilAndEqualTimestampTies(t *testing.T) {
	utc := time.Date(2026, 11, 24, 0, 0, 0, 123, time.UTC)
	zone := time.FixedZone("same-instant", 8*60*60)
	otherZone := utc.In(zone)
	end, otherEnd := utc.Add(time.Hour), otherZone.Add(time.Hour)
	for _, preferOAuth := range []bool{false, true} {
		for _, timestamps := range []string{"nil_before_used", "both_nil", "equal_other_zone"} {
			t.Run(fmt.Sprintf("oauth=%t/%s", preferOAuth, timestamps), func(t *testing.T) {
				accounts := gatewayLayer2Accounts(2)
				accounts[1].Type = AccountTypeOAuth
				accounts[0].SessionWindowEnd, accounts[1].SessionWindowEnd = &end, &otherEnd
				if timestamps == "nil_before_used" {
					accounts[1].LastUsedAt = &utc
				} else if timestamps == "equal_other_zone" {
					accounts[0].LastUsedAt, accounts[1].LastUsedAt = &utc, &otherZone
				}
				seen := map[int64]bool{}
				for trial := 0; trial < 64; trial++ {
					available := []accountWithLoad{
						{account: &accounts[0], loadInfo: &AccountLoadInfo{}},
						{account: &accounts[1], loadInfo: &AccountLoadInfo{}},
					}
					eligible := gatewayLayer2LegacyEligible(available, true, preferOAuth)
					queue := newLoadAwareSelectionQueue(available, true, preferOAuth)
					selected, ok := queue.next(utc)
					require.True(t, ok)
					require.True(t, eligible[selected.account.ID])
					seen[selected.account.ID] = true
				}
				if timestamps == "nil_before_used" {
					require.Equal(t, map[int64]bool{1: true}, seen, "nil LRU beats a used OAuth account")
				} else if preferOAuth {
					require.Equal(t, map[int64]bool{2: true}, seen, "OAuth preference applies to complete LRU ties")
				} else {
					require.Len(t, seen, 2, "equal instants in different zones must share one random tie")
				}
			})
		}
	}
}
