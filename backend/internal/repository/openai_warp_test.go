package repository

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWarpOverridesAccountProxy(t *testing.T) {
	warp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("warp")) }))
	defer warp.Close()
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("other")) }))
	defer other.Close()
	t.Setenv("OPENAI_WARP_PROXY_URL", warp.URL)
	ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/test", nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, other.URL, 1, 1)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "warp", string(body))
}

func TestOpenAIWarpUnavailableFailsClosed(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	t.Setenv("OPENAI_WARP_PROXY_URL", "http://"+address)
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("other")) }))
	defer other.Close()
	ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/test", nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, other.URL, 1, 1)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestNonOpenAIProxyIsNotOverridden(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "http://127.0.0.1:1")
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("other")) }))
	defer other.Close()
	ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileGrok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/test", nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, other.URL, 1, 1)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "other", string(body))
}

// Grok raw chat completions uses an OpenAI-compatible transport profile, but
// the selected account platform must still preserve the Grok account proxy.
func TestOpenAICompatibleProfileKeepsGrokAccountProxy(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "http://127.0.0.1:1")
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("grok")) }))
	defer other.Close()
	ctx := service.WithHTTPUpstreamProfile(context.Background(), service.HTTPUpstreamProfileOpenAI)
	ctx = service.WithUpstreamAccountPlatform(ctx, service.PlatformGrok)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com/test", nil)
	require.NoError(t, err)
	resp, err := NewHTTPUpstream(nil).Do(req, other.URL, 1, 1)
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "grok", string(body))
}
