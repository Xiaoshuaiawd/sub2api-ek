//go:build unit

package service

import (
	"bytes"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type selectionLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *selectionLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *selectionLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureSelectionLog(t *testing.T) *selectionLogBuffer {
	t.Helper()
	buf := &selectionLogBuffer{}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return buf
}

// The storage order of the candidates must not decide the winner: the newest
// starts_at wins, and the highest id breaks a tie. The rows below are handed over
// in an order where "first row" (id 9), "highest id" (id 9) and "last row" (id 2)
// all produce a different answer than the required rule (id 5).
func TestSelectActiveSubscriptionCandidatePrefersNewestStartThenHighestID(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	olderButHighestID := UserSubscription{
		ID:            9,
		StartsAt:      now.Add(-48 * time.Hour),
		ExpiresAt:     now.AddDate(0, 0, 3),
		DailyUsageUSD: 7.5,
	}
	tiedHigherID := UserSubscription{
		ID:            5,
		StartsAt:      now,
		ExpiresAt:     now.AddDate(0, 0, 20),
		DailyUsageUSD: 2.25,
	}
	tiedLowerID := UserSubscription{
		ID:            2,
		StartsAt:      now,
		ExpiresAt:     now.AddDate(0, 0, 20),
		DailyUsageUSD: 4.75,
	}
	captureSelectionLog(t)

	picked, ok := SelectActiveSubscriptionCandidate(1001, 7, []UserSubscription{
		olderButHighestID, tiedHigherID, tiedLowerID,
	})
	require.True(t, ok)
	require.Equal(t, int64(5), picked.ID)
	require.Equal(t, 2.25, picked.DailyUsageUSD)
	require.True(t, picked.ExpiresAt.Equal(tiedHigherID.ExpiresAt))
}

func TestSelectActiveSubscriptionCandidateReportsEveryCandidateInWarnLog(t *testing.T) {
	now := time.Now().Truncate(time.Second)
	buf := captureSelectionLog(t)

	picked, ok := SelectActiveSubscriptionCandidate(4242, 7, []UserSubscription{
		{ID: 5, StartsAt: now},
		{ID: 8, StartsAt: now},
		{ID: 6, StartsAt: now.Add(-time.Hour)},
	})
	require.True(t, ok)
	require.Equal(t, int64(8), picked.ID)

	out := buf.String()
	require.Contains(t, out, `"level":"WARN"`)
	require.Contains(t, out, `"user_id":4242`)
	require.Contains(t, out, `"group_id":7`)
	require.Contains(t, out, `"candidate_subscription_ids":[5,8,6]`)
	require.Contains(t, out, `"selected_subscription_id":8`)
}

func TestSelectActiveSubscriptionCandidateKeepsSingleMatchSilent(t *testing.T) {
	buf := captureSelectionLog(t)
	now := time.Now().Truncate(time.Second)

	picked, ok := SelectActiveSubscriptionCandidate(4242, 7, []UserSubscription{{ID: 5, StartsAt: now}})
	require.True(t, ok)
	require.Equal(t, int64(5), picked.ID)
	require.NotContains(t, buf.String(), "WARN")
}

func TestSelectActiveSubscriptionCandidateReportsNoMatch(t *testing.T) {
	picked, ok := SelectActiveSubscriptionCandidate(1001, 7, nil)
	require.False(t, ok)
	require.Nil(t, picked)
}
