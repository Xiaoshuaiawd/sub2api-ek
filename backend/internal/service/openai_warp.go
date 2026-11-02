package service

import (
	"context"
	"os"
	"strings"
)

type upstreamAccountPlatformKey struct{}

// WithUpstreamAccountPlatform preserves provider-specific transport profiles
// while distinguishing actual OpenAI accounts from compatible providers.
func WithUpstreamAccountPlatform(ctx context.Context, platform string) context.Context {
	return context.WithValue(ctx, upstreamAccountPlatformKey{}, platform)
}

func OpenAIWarpApplies(ctx context.Context, profile HTTPUpstreamProfile) bool {
	platform, marked := ctx.Value(upstreamAccountPlatformKey{}).(string)
	if marked {
		return platform == PlatformOpenAI
	}
	return IsOpenAIWarpProfile(profile)
}

// IsKnownNonOpenAIAccount identifies compatibility routes which must not
// inherit OpenAI-only WARP or Codex cookie-pool policy.
func IsKnownNonOpenAIAccount(ctx context.Context) bool {
	platform, marked := ctx.Value(upstreamAccountPlatformKey{}).(string)
	return marked && platform != PlatformOpenAI
}

// OpenAIWarpProxyURL is the mandatory server-side egress for OpenAI accounts.
func OpenAIWarpProxyURL() string {
	if value := strings.TrimSpace(os.Getenv("OPENAI_WARP_PROXY_URL")); value != "" {
		return value
	}
	return "socks5h://warp-proxy:1080"
}

// OpenAIWarpWorkerProxyURL is reachable from the separate login worker. It
// should point at the same WARP sidecar as the application-side URL.
func OpenAIWarpWorkerProxyURL() string {
	if value := strings.TrimSpace(os.Getenv("OPENAI_WARP_WORKER_PROXY_URL")); value != "" {
		return value
	}
	return OpenAIWarpProxyURL()
}

func IsOpenAIWarpProfile(profile HTTPUpstreamProfile) bool {
	return profile == HTTPUpstreamProfileOpenAI || profile == HTTPUpstreamProfileOpenAIHarvest || profile == HTTPUpstreamProfileExcelBPS
}
