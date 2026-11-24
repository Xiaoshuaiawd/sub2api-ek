//go:build unit

package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type fullAccountWaitQueueCache struct {
	fakeConcurrencyCache
	waitCalls  int
	maxWait    int
	decrements int
}

func (s *fullAccountWaitQueueCache) AcquireAccountSlot(context.Context, int64, int, string) (bool, error) {
	return false, nil
}

func (s *fullAccountWaitQueueCache) IncrementAccountWaitCount(_ context.Context, _ int64, maximum int) (bool, error) {
	s.waitCalls++
	s.maxWait = maximum
	return false, nil
}

func (s *fullAccountWaitQueueCache) DecrementAccountWaitCount(context.Context, int64) error {
	s.decrements++
	return nil
}

func TestGatewayCompatibilityAccountWaitPlanRejectsFullQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"responses", "chat/completions"} {
		for _, stream := range []bool{false, true} {
			name := endpoint + "/json"
			if stream {
				name = endpoint + "/stream"
			}
			t.Run(name, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Gateway.Scheduling.FallbackMaxWaiting = 2
				cfg.Gateway.Scheduling.FallbackWaitTimeout = time.Second
				groupID := int64(98001)
				group := &service.Group{ID: groupID, Hydrated: true, Platform: service.PlatformAnthropic, Status: service.StatusActive, RateMultiplier: 1}
				account := &service.Account{
					ID: 98002, Platform: service.PlatformAnthropic, Type: service.AccountTypeAPIKey,
					Status: service.StatusActive, Schedulable: true, Concurrency: 1,
					Credentials:   map[string]any{"api_key": "synthetic-key"},
					AccountGroups: []service.AccountGroup{{AccountID: 98002, GroupID: groupID}},
				}
				cache := &fullAccountWaitQueueCache{}
				concurrency := service.NewConcurrencyService(cache)
				snapshot := service.NewSchedulerSnapshotService(&fakeSchedulerCache{accounts: []*service.Account{account}}, nil, nil, nil, nil)
				billingCache := service.NewBillingCacheService(newHandlerInflightCache(100), nil, nil, nil, nil, nil, cfg, nil)
				t.Cleanup(billingCache.Stop)
				upstream := &compatStreamUsageUpstream{}
				gw := service.NewGatewayService(
					nil, &fakeGroupRepo{group: group}, nil, nil, nil, nil, nil, nil, cfg,
					snapshot, concurrency, service.NewBillingService(cfg, nil), nil, nil, nil, upstream,
					&service.DeferredService{}, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
				)
				h := &GatewayHandler{gatewayService: gw, billingCacheService: billingCache, cfg: cfg, maxAccountSwitches: 1,
					concurrencyHelper: NewConcurrencyHelper(concurrency, SSEPingFormatComment, 0)}
				body := `{"model":"claude-sonnet-4.5","input":"hello","stream":false}`
				if endpoint == "chat/completions" {
					body = `{"model":"claude-sonnet-4.5","messages":[{"role":"user","content":"hello"}],"stream":false}`
				}
				if stream {
					body = strings.Replace(body, `"stream":false`, `"stream":true`, 1)
				}
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
				defer cancel()
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/"+endpoint, strings.NewReader(body)).WithContext(ctx)
				c.Request.Header.Set("Content-Type", "application/json")
				key := &service.APIKey{ID: 98003, UserID: 98004, GroupID: &groupID, Group: group, User: &service.User{ID: 98004, Balance: 100}}
				c.Set(string(middleware.ContextKeyAPIKey), key)
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: key.UserID})
				if endpoint == "responses" {
					h.Responses(c)
				} else {
					h.ChatCompletions(c)
				}
				require.Equal(t, http.StatusTooManyRequests, rec.Code, rec.Body.String())
				require.Equal(t, gatewayQueueFullCode, gjson.Get(rec.Body.String(), "error.code").String(), rec.Body.String())
				require.NotContains(t, rec.Header().Get("Content-Type"), "text/event-stream")
				require.Equal(t, 1, cache.waitCalls)
				require.Equal(t, 2, cache.maxWait)
				require.Zero(t, cache.decrements, "a rejected waiter never owned a queue position")
				require.Zero(t, upstream.calls)
			})
		}
	}
}

type countedAccountWaitPlanCache struct {
	helperConcurrencyCacheStub
	queued       int
	maximum      int
	increments   int
	decrements   int
	onCounted    func()
	cleanupErr   error
	incrementErr error
}

func (s *countedAccountWaitPlanCache) IncrementAccountWaitCount(_ context.Context, _ int64, maximum int) (bool, error) {
	s.mu.Lock()
	s.maximum = maximum
	s.increments++
	if s.incrementErr != nil {
		err := s.incrementErr
		s.mu.Unlock()
		return false, err
	}
	allowed := s.queued < maximum
	if allowed {
		s.queued++
	}
	hook := s.onCounted
	s.mu.Unlock()
	if allowed && hook != nil {
		hook()
	}
	return allowed, nil
}

func (s *countedAccountWaitPlanCache) DecrementAccountWaitCount(ctx context.Context, _ int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cleanupErr = ctx.Err()
	s.queued--
	s.decrements++
	return nil
}

func TestAccountWaitPlanOwnsOnlyItsQueuedPosition(t *testing.T) {
	for _, name := range []string{"immediate", "full", "acquired", "timeout", "cancel"} {
		t.Run(name, func(t *testing.T) {
			cache := &countedAccountWaitPlanCache{}
			switch name {
			case "immediate":
				cache.accountSeq = []bool{true}
				cache.queued = 1
			case "full":
				cache.accountSeq = []bool{false}
				cache.queued = 1
			case "acquired":
				cache.accountSeq = []bool{false, true}
			}
			helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatComment, 0)
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			if name == "cancel" {
				cache.onCounted = cancel
			}
			started := false
			release, err := helper.AcquireAccountSlotWithWaitPlan(c, 123, &service.AccountWaitPlan{
				MaxConcurrency: 1, MaxWaiting: 1, Timeout: 15 * time.Millisecond,
			}, false, &started)
			switch name {
			case "immediate":
				require.NoError(t, err)
				require.NotNil(t, release)
				require.Zero(t, cache.increments)
				require.Zero(t, cache.decrements)
				require.Equal(t, 1, cache.queued)
			case "full":
				var full *WaitQueueFullError
				require.ErrorAs(t, err, &full)
				require.Nil(t, release)
				require.Equal(t, 1, cache.increments)
				require.Zero(t, cache.decrements)
				require.Equal(t, 1, cache.queued)
			default:
				require.Equal(t, 1, cache.increments)
				require.Equal(t, 1, cache.decrements)
				require.Zero(t, cache.queued)
				require.NoError(t, cache.cleanupErr, "counter cleanup is independent of a canceled request")
				if name == "acquired" {
					require.NoError(t, err)
					require.NotNil(t, release)
				}
				if name == "timeout" {
					var timeout *ConcurrencyError
					require.ErrorAs(t, err, &timeout)
					require.True(t, timeout.IsTimeout)
				}
				if name == "cancel" {
					require.ErrorIs(t, err, context.Canceled)
				}
			}
			if release != nil {
				release()
				require.Equal(t, 1, cache.accountReleaseCalls)
			}
			require.False(t, started)
		})
	}
}

func TestAccountWaitPlanIncrementFailureDoesNotReleaseAnotherWaiter(t *testing.T) {
	failure := errors.New("synthetic account wait counter failure")
	cache := &countedAccountWaitPlanCache{queued: 1, incrementErr: failure}
	helper := NewConcurrencyHelper(service.NewConcurrencyService(cache), SSEPingFormatComment, 0)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	started := false
	release, err := helper.AcquireAccountSlotWithWaitPlan(c, 123, &service.AccountWaitPlan{
		MaxConcurrency: 1, MaxWaiting: 2, Timeout: 10 * time.Millisecond,
	}, false, &started)
	require.Equal(t, 1, cache.queued, "an unconfirmed increment must not release another request's queue position")
	require.Zero(t, cache.decrements)
	require.ErrorIs(t, err, failure)
	require.Nil(t, release)
	require.False(t, started)
}
