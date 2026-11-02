package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIWarpWorkerAddress(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "socks5h://server-warp:1080")
	t.Setenv("OPENAI_WARP_WORKER_PROXY_URL", "")
	require.Equal(t, "socks5h://server-warp:1080", OpenAIWarpWorkerProxyURL())
	t.Setenv("OPENAI_WARP_WORKER_PROXY_URL", "socks5h://worker-warp:1080")
	require.Equal(t, "socks5h://worker-warp:1080", OpenAIWarpWorkerProxyURL())
	require.Equal(t, "socks5h://server-warp:1080", OpenAIWarpProxyURL())
}

func TestOpenAIWarpUsesAccountPlatformBeforeTransportProfile(t *testing.T) {
	openAI := WithUpstreamAccountPlatform(context.Background(), PlatformOpenAI)
	grok := WithUpstreamAccountPlatform(context.Background(), PlatformGrok)
	require.True(t, OpenAIWarpApplies(openAI, HTTPUpstreamProfileLongStream))
	require.True(t, OpenAIWarpApplies(openAI, HTTPUpstreamProfileGrok))
	require.False(t, OpenAIWarpApplies(grok, HTTPUpstreamProfileOpenAI))
}

func TestOpenAIWarpAppliesToAccountWebSearchProxy(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "socks5h://warp.example:1080")
	proxy := &Proxy{ID: 1, Protocol: "http", Host: "legacy.example", Port: 8080}
	account := &Account{Platform: PlatformOpenAI, ProxyID: &proxy.ID, Proxy: proxy}
	require.Equal(t, OpenAIWarpProxyURL(), resolveAccountProxyURL(account))
	account.Platform = PlatformGrok
	require.Equal(t, proxy.URL(), resolveAccountProxyURL(account))
}

func TestOpenAIWarpRejectsUncontrolledPrism(t *testing.T) {
	service := &OpenAIGatewayService{}
	body, _, _, err := service.callPrismBrowserForCaller(context.Background(), &Account{Platform: PlatformOpenAI}, nil, "", "")
	require.ErrorContains(t, err, "cannot guarantee WARP-only egress")
	require.Nil(t, body)
}

func TestOpenAIWarpRejectsUncontrolledPlugin(t *testing.T) {
	manager := &PluginManager{}
	manager.route.Store(&pluginRoute{rolloutPercent: 100})
	response, handled, err := manager.RoundTripOpenAIOAuth(context.Background(), nil, "http://legacy-proxy", &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
	require.True(t, handled)
	require.ErrorContains(t, err, "cannot guarantee WARP-only egress")
	require.Nil(t, response)
}

func TestOpenAIWarpRejectsTicketWithoutVerifiedEgress(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "socks5h://warp-proxy:1080")
	s := &OpenAIGatewayService{}
	_, release, err := s.pinStoredCodexTicketEgress(context.Background(), &openAICodexTicket{}, "")
	require.ErrorIs(t, err, ErrOpenAICodexTicketUnavailable)
	release()
}
