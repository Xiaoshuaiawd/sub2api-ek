//go:build unit

package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWarpOffRestoresAccountProxyAndRejectsWarpTicket(t *testing.T) {
	previous := OpenAIWarpEnabled()
	setOpenAIWarpEnabled(false)
	t.Cleanup(func() { setOpenAIWarpEnabled(previous) })
	proxy := &Proxy{ID: 77, Protocol: "http", Host: "legacy.example", Port: 8080}
	account := &Account{Platform: PlatformOpenAI, ProxyID: &proxy.ID, Proxy: proxy}
	require.Equal(t, proxy.URL(), resolveAccountProxyURL(account))
	require.Equal(t, proxy.URL(), openAIAccountProxyURL(account))
	require.Equal(t, proxy.URL(), openAIWarpOrProxyURL(proxy.URL()))
	require.False(t, OpenAIWarpApplies(WithUpstreamAccountPlatform(context.Background(), PlatformOpenAI), HTTPUpstreamProfileOpenAI))
	ticket := &openAICodexTicket{HarvestProxyURL: OpenAIWarpProxyURL(), ExpiresAt: time.Now().Add(time.Minute)}
	require.False(t, ticket.valid(time.Now(), 292))
	attempt := codexHarvestAttempt{proxy: proxy.URL()}
	bindCodexHarvestEgress(ticket, attempt, "legacy-session")
	require.Equal(t, proxy.URL(), ticket.HarvestProxyURL)
	require.Equal(t, "legacy-session", ticket.HarvestSessionID)
	gateway := &OpenAIGatewayService{}
	pinned, release, err := gateway.pinStoredCodexTicketEgress(context.Background(), ticket, "")
	require.NoError(t, err)
	release()
	require.Equal(t, proxy.URL(), pinned)

	upstream := &runtimeFallbackUpstream{do: func(_ *http.Request, gotProxy string, _ int64, _ int) (*http.Response, error) {
		require.Equal(t, proxy.URL(), gotProxy)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}, nil
	}}
	gateway.httpUpstream = upstream
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://chatgpt.com/backend-api/codex/responses", nil)
	require.NoError(t, err)
	response, err := gateway.doUpstreamWithProxyFallback(context.Background(), req, account, proxy.URL())
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, response.StatusCode)
	require.NoError(t, response.Body.Close())
}
