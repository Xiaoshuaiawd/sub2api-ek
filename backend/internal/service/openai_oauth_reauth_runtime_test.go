package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/reauthruntime"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthReauthManagedWorkerNeedsNoEnvironmentToken(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "")
	t.Setenv("DATA_DIR", t.TempDir())
	s := &OpenAIOAuthReauthService{}
	cfg := &config.Config{}
	cfg.Server.Host = "0.0.0.0"
	cfg.Server.Port = 4040
	s.configureWorker(cfg, BuildInfo{Version: "2.9.4"})
	defer s.stopWorker()
	require.NotNil(t, s.worker)
	require.Len(t, s.workerToken, 64)
	configured, valid := s.WorkerAuthentication(s.workerToken)
	require.True(t, configured)
	require.True(t, valid)
	_, valid = s.WorkerAuthentication(strings.Repeat("x", 64))
	require.False(t, valid)
	status := s.WorkerStatus()
	require.Equal(t, "managed", status.Mode)
	require.Equal(t, "idle", status.State)
	raw, err := json.Marshal(status)
	require.NoError(t, err)
	require.NotContains(t, string(raw), s.workerToken)
	require.Error(t, s.checkWorkerMode(OpenAIOAuthReauthModeEmailOTPURL))
}

func TestOpenAIOAuthReauthPreservesExternalWorker(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", strings.Repeat("a", 32))
	s := &OpenAIOAuthReauthService{}
	s.configureWorker(nil, BuildInfo{Version: "2.9.4"})
	require.Nil(t, s.worker)
	configured, valid := s.WorkerAuthentication(strings.Repeat("a", 32))
	require.True(t, configured)
	require.True(t, valid)
	require.Equal(t, "external_offline", s.WorkerStatus().Reason)
}

func TestOpenAIOAuthReauthWorkerHeartbeatKeepsExternalWorkerAvailable(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", strings.Repeat("a", 32))
	s := &OpenAIOAuthReauthService{}
	s.configureWorker(nil, BuildInfo{Version: "2.9.4"})
	s.RecordWorkerHeartbeat("worker", true)
	require.Equal(t, "running", s.WorkerStatus().State)
}

func TestOpenAIOAuthReauthShortExplicitTokenDoesNotStartCompetingWorker(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", "short")
	s := &OpenAIOAuthReauthService{}
	s.configureWorker(nil, BuildInfo{Version: "2.9.4"})
	require.Nil(t, s.worker)
	configured, valid := s.WorkerAuthentication("short")
	require.False(t, configured)
	require.False(t, valid)
	require.Equal(t, "external_not_configured", s.WorkerStatus().Reason)
}

func TestExternalWorkerHeartbeatIsolationStateAndRecovery(t *testing.T) {
	t.Setenv("OPENAI_REAUTH_WORKER_TOKEN", strings.Repeat("a", 32))
	s := &OpenAIOAuthReauthService{}
	now := time.Now()
	s.recordWorkerHeartbeatAt("", true, now)
	require.Equal(t, "warp_isolation_required", s.WorkerStatus().Reason)
	s.recordWorkerHeartbeatAt(strings.Repeat("x", 129), true, now)
	require.Equal(t, "warp_isolation_required", s.WorkerStatus().Reason)
	s.recordWorkerHeartbeatAt("old", false, now)
	require.Equal(t, "warp_isolation_required", s.WorkerStatus().Reason)
	s.recordWorkerHeartbeatAt("new", true, now)
	require.Equal(t, "running", s.WorkerStatus().State)
	// A legacy heartbeat cannot hide a different healthy worker.
	s.recordWorkerHeartbeatAt("", false, now)
	s.recordWorkerHeartbeatAt("old", false, now)
	require.Equal(t, "running", s.WorkerStatus().State)
	// With the same clock reading, a declaration change still takes effect.
	s.recordWorkerHeartbeatAt("new", false, now)
	require.Equal(t, "warp_isolation_required", s.WorkerStatus().Reason)
	s.recordWorkerHeartbeatAt("new", true, now)
	require.Equal(t, "running", s.WorkerStatus().State)
	isolated, unisolated := s.workerHeartbeatSummary(now.Add(27*time.Minute), 0)
	require.False(t, isolated)
	require.False(t, unisolated)
	require.Equal(t, "external_offline", s.WorkerStatus().Reason)
}

func TestManagedWorkerHeartbeatIsolationStateAndRecovery(t *testing.T) {
	now := time.Now()
	started := now.Add(-2 * time.Second).UnixNano()
	running := reauthruntime.Status{Mode: "managed", State: "running", StartedAt: started}
	s := &OpenAIOAuthReauthService{}
	s.recordWorkerHeartbeatAt("previous", true, now.Add(-3*time.Second))
	s.recordWorkerHeartbeatAt("at-start", true, time.Unix(0, started))
	isolated, unisolated := s.workerHeartbeatSummary(now, started)
	require.Equal(t, "preparing", managedWorkerHeartbeatStatus(running, isolated, unisolated, now).State)
	s.recordWorkerHeartbeatAt("current", false, now.Add(-time.Second))
	isolated, unisolated = s.workerHeartbeatSummary(now, started)
	status := managedWorkerHeartbeatStatus(running, isolated, unisolated, now)
	require.Equal(t, "unavailable", status.State)
	require.Equal(t, "warp_isolation_required", status.Reason)
	s.recordWorkerHeartbeatAt("current", true, now)
	isolated, unisolated = s.workerHeartbeatSummary(now, started)
	status = managedWorkerHeartbeatStatus(running, isolated, unisolated, now)
	require.Equal(t, "running", status.State)
	require.Empty(t, status.Reason)
}

func TestWorkerHeartbeatMapIsBoundedAndDoesNotEvictHealthyWorkers(t *testing.T) {
	s := &OpenAIOAuthReauthService{}
	now := time.Now()
	for i := 0; i < workerHeartbeatLimit; i++ {
		s.recordWorkerHeartbeatAt(fmt.Sprintf("healthy-%d", i), true, now)
	}
	s.recordWorkerHeartbeatAt("", false, now)
	require.Len(t, s.workerHeartbeats, workerHeartbeatLimit)
	isolated, unisolated := s.workerHeartbeatSummary(now, 0)
	require.True(t, isolated)
	require.False(t, unisolated)
	// Updating an existing worker remains possible even at the bound.
	s.recordWorkerHeartbeatAt("healthy-0", false, now)
	_, unisolated = s.workerHeartbeatSummary(now, 0)
	require.True(t, unisolated)
}
