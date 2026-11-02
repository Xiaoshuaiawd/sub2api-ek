package repository

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/mihomo"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func warpAstraRequest(req *http.Request) *http.Request {
	return req.WithContext(service.WithUpstreamAccountPlatform(req.Context(), service.PlatformOpenAI))
}

func consumeAffinityResponse(t *testing.T, resp *http.Response, err error) {
	t.Helper()
	require.NoError(t, err)
	require.NotNil(t, resp)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
}

func TestAstraGatewayIPAffinity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		enabled bool
		source  string
	}{
		{"off", false, "http://source.invalid:80"},
		{"on", true, "http://source.invalid:80"},
		{"direct", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Gateway.CodexGatewayPin = pinConfig()
			cfg.Gateway.CodexGatewayPin.IPAffinity = tc.enabled
			calls := 0
			wrapper := &astraRoutingUpstream{cfg: cfg}
			wrapper.delegate = gatewayPinDelegate{call: func(r *http.Request, proxy string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
				if id == 299 {
					return pinResponse("__oailb=source-route; Path=/; Secure; Max-Age=230"), nil
				}
				require.Equal(t, int64(300), id)
				require.Equal(t, service.OpenAIWarpProxyURL(), proxy)
				require.Equal(t, "Bearer target-test", r.Header.Get("Authorization"))
				require.Equal(t, "target-account", r.Header.Get("ChatGPT-Account-ID"))
				cookie, err := r.Cookie("__oailb")
				require.NoError(t, err)
				require.Equal(t, "source-route", cookie.Value)
				calls++
				return pinResponse(""), nil
			}}
			resp, err := wrapper.Do(warpAstraRequest(pinRequest(t)), tc.source, 299, 1)
			consumeAffinityResponse(t, resp, err)
			target := warpAstraRequest(pinRequest(t))
			target.Header.Set("Authorization", "Bearer target-test")
			target.Header.Set("ChatGPT-Account-ID", "target-account")
			resp, err = wrapper.Do(target, "http://target.invalid:80", 300, 1)
			consumeAffinityResponse(t, resp, err)
			require.Equal(t, 1, calls, "production forwarding sends the target request once without a quality probe")
			cookie, proxy, release, err := wrapper.CodexGatewayPinWSRequest(t.Context(), target.Header, service.OpenAIWarpProxyURL(), 300, 1)
			require.NoError(t, err)
			release()
			require.Equal(t, "source-route", cookie)
			require.Equal(t, service.OpenAIWarpProxyURL(), proxy, "WS must dial the validated egress")
			require.Equal(t, 1, calls, "WS does not add a production quality probe")
			raw, err := json.Marshal(wrapper.AstraGatewaySnapshot(t.Context()))
			require.NoError(t, err)
			require.NotContains(t, string(raw), "source.invalid")
		})
	}
}

func TestAstraGatewayIPAffinityRevalidatesChangedEgress(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	cfg.Gateway.CodexGatewayPin.IPAffinity = true
	wrapper := &astraRoutingUpstream{cfg: cfg}
	var proxies []string
	wrapper.delegate = gatewayPinDelegate{call: func(_ *http.Request, p string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		if id == 299 {
			return pinResponse("__oailb=same-cookie; Path=/; Secure; Max-Age=230"), nil
		}
		proxies = append(proxies, p)
		return pinResponse(""), nil
	}}
	resp, err := wrapper.Do(warpAstraRequest(pinRequest(t)), "old-source", 299, 1)
	consumeAffinityResponse(t, resp, err)
	resp, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "target", 300, 1)
	consumeAffinityResponse(t, resp, err)
	pool := wrapper.current(t.Context())
	expiry := pool.routes[299].expires
	require.Equal(t, "ready", wrapper.AstraGatewaySnapshot(t.Context()).Targets[0].State)
	resp, err = wrapper.Do(warpAstraRequest(pinRequest(t)), "new-source", 299, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, expiry, pool.routes[299].expires)
	require.Equal(t, "ready", wrapper.AstraGatewaySnapshot(t.Context()).Targets[0].State)
	resp, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "target", 300, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, []string{service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL()}, proxies)
	// A backup source must bring its own egress along with its Cookie.
	backup := codexGatewayRoute{cookie: http.Cookie{Name: "__oailb", Value: "backup", Path: "/"}, proxy: service.OpenAIWarpProxyURL(), expires: time.Now().Add(time.Minute)}
	pool.recordSource(298, time.Now(), &backup, true)
	pool.recordSource(299, time.Now(), nil, false)
	resp, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "target", 300, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, []string{service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL()}, proxies[6:])
	// Changing the legacy affinity setting cannot change the WARP route.
	cfg.Gateway.CodexGatewayPin.IPAffinity = false
	require.Same(t, pool, wrapper.current(t.Context()), "legacy affinity setting cannot change the WARP route")
	resp, err = wrapper.Do(warpAstraRequest(pinRequest(t)), "new-source", 299, 1)
	consumeAffinityResponse(t, resp, err)
	resp, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "target", 300, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, []string{service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL(), service.OpenAIWarpProxyURL()}, proxies[9:])
}

func TestAstraGatewayIPAffinityRejectsFailedTarget(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	cfg.Gateway.CodexGatewayPin.IPAffinity = true
	calls := 0
	wrapper := &astraRoutingUpstream{cfg: cfg, delegate: gatewayPinDelegate{call: func(_ *http.Request, proxy string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		if id == 299 {
			return pinResponse("__oailb=route; Path=/; Secure; Max-Age=230"), nil
		}
		require.Equal(t, service.OpenAIWarpProxyURL(), proxy)
		calls++
		return &http.Response{StatusCode: 403, Body: http.NoBody}, nil
	}}}
	resp, err := wrapper.Do(warpAstraRequest(pinRequest(t)), "source", 299, 1)
	consumeAffinityResponse(t, resp, err)
	_, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "target", 300, 1)
	require.Error(t, err)
	require.Equal(t, 1, calls, "do not forward actual target request after failure")
	_, _, _, err = wrapper.CodexGatewayPinWSRequest(t.Context(), http.Header{}, service.OpenAIWarpProxyURL(), 300, 1)
	require.NoError(t, err)
	require.Equal(t, 1, calls, "WS forwarding does not add a production probe")
}

func TestAstraOpenAIUsesWarpWhenLegacyNodePoolUnavailable(t *testing.T) {
	t.Setenv("OPENAI_WARP_PROXY_URL", "http://warp.invalid:8080")
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	cfg.Gateway.CodexGatewayPin.RotateNodes = true
	cfg.Gateway.CodexGatewayPin.IPAffinity = false
	nodeQueries := 0
	wrapper := &astraRoutingUpstream{cfg: cfg}
	wrapper.listNodes = func() ([]mihomo.AstraNode, error) {
		nodeQueries++
		return nil, errors.New("legacy node pool unavailable")
	}
	wrapper.pinNode = func(context.Context, mihomo.AstraNode) (string, func(), error) {
		t.Fatal("OpenAI must not lease a legacy node")
		return "", nil, nil
	}
	var targetCalls int
	wrapper.delegate = gatewayPinDelegate{call: func(r *http.Request, proxy string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		require.Equal(t, "http://warp.invalid:8080", proxy)
		if id == 299 {
			return pinResponse("__oailb=source-route; Path=/; Secure; Max-Age=230"), nil
		}
		require.Equal(t, int64(300), id)
		cookie, err := r.Cookie("__oailb")
		require.NoError(t, err)
		require.Equal(t, "source-route", cookie.Value)
		targetCalls++
		return pinResponse(""), nil
	}}
	resp, err := wrapper.Do(warpAstraRequest(pinRequest(t)), "http://old-source.invalid:8080", 299, 1)
	consumeAffinityResponse(t, resp, err)
	resp, err = wrapper.Do(warpAstraRequest(forceAstraProbe(pinRequest(t))), "http://old-target.invalid:8080", 300, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, 3, targetCalls, "two validation probes and one business request")
	require.Zero(t, nodeQueries)
	pool := wrapper.current(t.Context())
	require.False(t, pool.config.RotateNodes)
	require.True(t, pool.config.IPAffinity)
	require.Equal(t, "http://warp.invalid:8080", pool.routes[299].proxy)
	require.Equal(t, "ready", wrapper.AstraGatewaySnapshot(t.Context()).Targets[0].State)
}

func TestAstraRoutingNonOpenAICompatibleRequestKeepsProxyAndCookie(t *testing.T) {
	cfg := &config.Config{}
	cfg.Gateway.CodexGatewayPin = pinConfig()
	cfg.Gateway.CodexGatewayPin.RotateNodes = true
	wrapper := &astraRoutingUpstream{cfg: cfg}
	calls := 0
	wrapper.delegate = gatewayPinDelegate{call: func(req *http.Request, proxy string, id int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
		calls++
		require.Equal(t, int64(300), id)
		require.Equal(t, "http://grok-proxy.invalid:8080", proxy)
		require.Equal(t, "__oailb=original", req.Header.Get("Cookie"))
		return pinResponse(""), nil
	}}
	req := pinRequest(t)
	ctx := service.WithHTTPUpstreamProfile(req.Context(), service.HTTPUpstreamProfileOpenAI)
	ctx = service.WithUpstreamAccountPlatform(ctx, service.PlatformGrok)
	req = req.WithContext(ctx)
	req.Header.Set("Cookie", "__oailb=original")
	resp, err := wrapper.Do(req, "http://grok-proxy.invalid:8080", 300, 1)
	consumeAffinityResponse(t, resp, err)
	require.Equal(t, 1, calls)
	require.Nil(t, wrapper.pool, "non-OpenAI forwarding must not initialize the Astra pool")
}
