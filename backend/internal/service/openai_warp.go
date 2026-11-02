package service

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var openAIWarpEnabled = func() *atomic.Bool { v := new(atomic.Bool); v.Store(true); return v }()
var openAIWarpSettingsOwner atomic.Pointer[SettingService]
var openAIWarpSettingsExpires atomic.Int64
var openAIWarpSettingsMu sync.Mutex

const openAIWarpSettingsTTL = 2 * time.Second

func OpenAIWarpEnabled() bool {
	owner := openAIWarpSettingsOwner.Load()
	if owner == nil || owner.settingRepo == nil || (openAIWarpEnabled.Load() && time.Now().UnixNano() < openAIWarpSettingsExpires.Load()) {
		return openAIWarpEnabled.Load()
	}
	openAIWarpSettingsMu.Lock()
	defer openAIWarpSettingsMu.Unlock()
	if !openAIWarpEnabled.Load() || time.Now().UnixNano() >= openAIWarpSettingsExpires.Load() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		value, err := owner.settingRepo.GetValue(ctx, SettingKeyOpenAIWarpEnabled)
		cancel()
		if err != nil {
			// A failed refresh must never leave a stale opt-out routing traffic directly.
			openAIWarpEnabled.Store(true)
		} else {
			openAIWarpEnabled.Store(strings.TrimSpace(value) != "false")
		}
		openAIWarpSettingsExpires.Store(time.Now().Add(openAIWarpSettingsTTL).UnixNano())
	}
	return openAIWarpEnabled.Load()
}

func setOpenAIWarpEnabled(enabled bool) {
	openAIWarpSettingsMu.Lock()
	openAIWarpEnabled.Store(enabled)
	openAIWarpSettingsExpires.Store(time.Now().Add(openAIWarpSettingsTTL).UnixNano())
	openAIWarpSettingsMu.Unlock()
}

type upstreamAccountPlatformKey struct{}
type openAIWarpPolicyKey struct{}

// WithOpenAIWarpPolicy pins the egress policy for a request whose result is
// later bound to the exit that sent it (for example a Codex ticket).
func WithOpenAIWarpPolicy(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, openAIWarpPolicyKey{}, enabled)
}

// WithUpstreamAccountPlatform preserves provider-specific transport profiles
// while distinguishing actual OpenAI accounts from compatible providers.
func WithUpstreamAccountPlatform(ctx context.Context, platform string) context.Context {
	return context.WithValue(ctx, upstreamAccountPlatformKey{}, platform)
}

func OpenAIWarpApplies(ctx context.Context, profile HTTPUpstreamProfile) bool {
	platform, marked := ctx.Value(upstreamAccountPlatformKey{}).(string)
	if (marked && platform != PlatformOpenAI) || (!marked && !IsOpenAIWarpProfile(profile)) {
		return false
	}
	enabled, pinned := ctx.Value(openAIWarpPolicyKey{}).(bool)
	if !pinned {
		enabled = OpenAIWarpEnabled()
	}
	if marked {
		return enabled && platform == PlatformOpenAI
	}
	return enabled
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
