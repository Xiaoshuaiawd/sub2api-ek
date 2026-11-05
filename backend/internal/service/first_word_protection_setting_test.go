//go:build unit

package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type firstWordSettingRepo struct {
	SettingRepository
	value string
	err   error
}

func (r *firstWordSettingRepo) GetValue(_ context.Context, key string) (string, error) {
	if key != SettingKeyOpenAIFirstWordProtectionEnabled || r.value == "" {
		return "", ErrSettingNotFound
	}
	if r.err != nil {
		return "", r.err
	}
	return r.value, nil
}

func TestFirstWordProtectionKeepsLastKnownValueOnReadFailure(t *testing.T) {
	cfg := &config.Config{}
	repo := &firstWordSettingRepo{value: "true"}
	settings := NewSettingService(repo, cfg)
	require.True(t, settings.GetOpenAIFirstWordProtectionEnabled(context.Background(), false))
	repo.err = errors.New("database unavailable")
	settings.InvalidateOpenAIFirstWordProtectionCache()
	require.True(t, settings.GetOpenAIFirstWordProtectionEnabled(context.Background(), false))
}

func TestFirstWordProtectionAdminSettingOverridesConfigWithoutRestart(t *testing.T) {
	cfg := &config.Config{}
	repo := &firstWordSettingRepo{}
	settings := NewSettingService(repo, cfg)
	gateway := &OpenAIGatewayService{cfg: cfg, settingService: settings}
	require.False(t, gateway.firstWordProtectionEnabled())
	repo.value = "true"
	settings.InvalidateOpenAIFirstWordProtectionCache()
	require.True(t, gateway.firstWordProtectionEnabled())
	repo.value = "false"
	settings.InvalidateOpenAIFirstWordProtectionCache()
	require.False(t, gateway.firstWordProtectionEnabled())
}
