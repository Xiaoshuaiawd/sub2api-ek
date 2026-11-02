//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type openAIWarpToggleRepo struct {
	SettingRepository
	value string
}

func (r *openAIWarpToggleRepo) GetValue(context.Context, string) (string, error) {
	return r.value, nil
}

func TestOpenAIWarpToggleRefreshesOtherInstanceAndIgnoresStaleSave(t *testing.T) {
	previousOwner := openAIWarpSettingsOwner.Swap(nil)
	previousEnabled := openAIWarpEnabled.Load()
	previousExpiry := openAIWarpSettingsExpires.Load()
	t.Cleanup(func() {
		openAIWarpSettingsOwner.Store(previousOwner)
		openAIWarpEnabled.Store(previousEnabled)
		openAIWarpSettingsExpires.Store(previousExpiry)
	})
	repo := &openAIWarpToggleRepo{value: "false"}
	owner := &SettingService{settingRepo: repo}
	openAIWarpSettingsOwner.Store(owner)
	openAIWarpSettingsExpires.Store(0)
	require.False(t, OpenAIWarpEnabled())

	// Another instance enables WARP. A stale local opt-out must be rechecked.
	repo.value = "true"
	require.True(t, OpenAIWarpEnabled())

	// An older save finishes its cache refresh after the newer database write.
	owner.refreshCachedSettings(&SystemSettings{OpenAIWarpEnabled: false})
	require.True(t, OpenAIWarpEnabled())
}

func TestOpenAIWarpPolicySnapshotSurvivesSwitch(t *testing.T) {
	previous := openAIWarpEnabled.Load()
	t.Cleanup(func() { openAIWarpEnabled.Store(previous) })
	openAI := WithUpstreamAccountPlatform(context.Background(), PlatformOpenAI)
	before := WithOpenAIWarpPolicy(openAI, false)
	after := WithOpenAIWarpPolicy(openAI, true)
	openAIWarpEnabled.Store(true)
	require.False(t, OpenAIWarpApplies(before, HTTPUpstreamProfileOpenAI))
	openAIWarpEnabled.Store(false)
	require.True(t, OpenAIWarpApplies(after, HTTPUpstreamProfileOpenAI))
}
