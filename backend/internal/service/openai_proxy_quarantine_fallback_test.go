//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func quarantineFallbackFixture() (*OpenAIGatewayService, *Account) {
	proxy := proxyForTest(91, "primary.invalid", 8080)
	proxy.FallbackMode = FallbackModeDirect
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Proxy: proxy, ProxyID: i64(proxy.ID), Status: StatusActive, Schedulable: true}
	s := &OpenAIGatewayService{}
	s.openaiProxyStreamCircuit = newOpenAIProxyStreamCircuit(openAIProxyStreamCircuitSettings{
		failureThreshold: 1, failureWindow: time.Minute, quarantineTTL: time.Minute, maxEntries: 16,
	})
	s.getOpenAIProxyStreamCircuit().recordFailure(proxy.ID, time.Now())
	return s, account
}

func TestWarpAccountIgnoresBoundProxyQuarantine(t *testing.T) {
	s, account := quarantineFallbackFixture()
	for _, mode := range []string{FallbackModeNone, FallbackModeDirect, FallbackModeProxy} {
		account.Proxy.FallbackMode = mode
		require.False(t, s.isOpenAIProxyStreamQuarantined(context.Background(), account))
		require.False(t, s.isOpenAIProxyStreamQuarantined(
			withOpenAIProxyQuarantineTransport(context.Background(), OpenAIUpstreamTransportResponsesWebsocketV2Ingress), account))
	}
	require.True(t, s.getOpenAIProxyStreamCircuit().isBlocked(account.Proxy.ID, time.Now()))
}

func TestWarpRequestsDoNotChangeAccountProxyCircuit(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "http://warp.invalid:8080")
	s, account := quarantineFallbackFixture()
	account.Proxy.FallbackMode, account.Proxy.BackupProxyID = FallbackModeProxy, i64(92)
	backup := proxyForTest(92, "backup.invalid", 8080)
	s.proxyRepo = &fakeProxyLookup{byID: map[int64]*Proxy{92: backup}}
	var calls int
	s.httpUpstream = &runtimeFallbackUpstream{do: func(req *http.Request, proxy string, id int64, _ int) (*http.Response, error) {
		calls++
		require.Equal(t, "http://warp.invalid:8080", proxy)
		require.Equal(t, account.ID, id)
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, "one-post", string(body))
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}}
	req, err := http.NewRequest(http.MethodPost, "https://example.invalid", strings.NewReader("one-post"))
	require.NoError(t, err)
	resp, err := s.doOpenAIUpstream(req, account.Proxy.URL(), account)
	require.NoError(t, err)
	id, proxied := openAIResponseEgressProxyID(account, resp)
	require.Negative(t, id)
	require.False(t, proxied)
	s.clearOpenAIProxyStreamDisconnect(account, resp)
	require.True(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()), "WARP success must not heal the bound proxy")
	s.getOpenAIProxyStreamCircuit().recordSuccess(91)
	s.recordOpenAIProxyStreamDisconnect(account, errors.New("unexpected EOF"), "", resp)
	require.False(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()), "WARP stream failure must not quarantine the bound proxy")
	require.False(t, s.getOpenAIProxyStreamCircuit().isBlocked(92, time.Now()))
	require.Equal(t, 1, calls)
	require.Equal(t, int64(91), *account.ProxyID)
}

func TestQuarantineFallbackDirectDisconnectDoesNotBlameBoundProxy(t *testing.T) {
	s, account := quarantineFallbackFixture()
	s.getOpenAIProxyStreamCircuit().recordSuccess(91)
	req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
	resp := markOpenAIResponseEgress(&http.Response{}, req, 0)
	s.recordOpenAIProxyStreamDisconnect(account, errors.New("unexpected EOF"), "", resp)
	require.False(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()))
}

func TestQuarantineFallbackNoMidstreamReplay(t *testing.T) {
	s, account := quarantineFallbackFixture()
	var calls int
	s.httpUpstream = &runtimeFallbackUpstream{do: func(req *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 200, Body: &quarantineBrokenStream{}}, nil
	}}
	req, _ := http.NewRequest(http.MethodPost, "https://example.invalid", strings.NewReader("test"))
	resp, err := s.doOpenAIUpstream(req, account.Proxy.URL(), account)
	require.NoError(t, err)
	body, readErr := io.ReadAll(resp.Body)
	require.Equal(t, "partial-output", string(body))
	require.ErrorIs(t, readErr, io.ErrUnexpectedEOF)
	require.Equal(t, 1, calls)
}

type quarantineBrokenStream struct{ read bool }

func (s *quarantineBrokenStream) Read(p []byte) (int, error) {
	if s.read {
		return 0, io.ErrUnexpectedEOF
	}
	s.read = true
	return copy(p, "partial-output"), nil
}
func (s *quarantineBrokenStream) Close() error { return nil }

func TestQuarantineFallbackConcurrentDirectSuccessDoesNotClearPrimary(t *testing.T) {
	s, account := quarantineFallbackFixture()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequest(http.MethodGet, "https://example.invalid", nil)
			resp := markOpenAIResponseEgress(&http.Response{}, req, 0)
			s.clearOpenAIProxyStreamDisconnect(account, resp)
			s.recordOpenAIProxyStreamDisconnect(account, io.ErrUnexpectedEOF, "", resp)
			s.isOpenAIProxyStreamQuarantined(context.Background(), account)
		}()
	}
	wg.Wait()
	require.True(t, s.getOpenAIProxyStreamCircuit().isBlocked(91, time.Now()))
}
