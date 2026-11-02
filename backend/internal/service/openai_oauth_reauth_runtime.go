package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/reauthruntime"
)

func (s *OpenAIOAuthReauthService) configureWorker(cfg *config.Config, info BuildInfo) {
	token := strings.TrimSpace(os.Getenv("OPENAI_REAUTH_WORKER_TOKEN"))
	if token != "" {
		s.workerToken = token
		return
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return
	}
	s.workerToken = hex.EncodeToString(buf)
	dir := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if dir == "" {
		if st, err := os.Stat("/app/data"); err == nil && st.IsDir() {
			dir = "/app/data"
		} else {
			dir = "./data"
		}
	}
	host := "127.0.0.1"
	port := 8080
	if cfg != nil {
		port = cfg.Server.Port
		switch cfg.Server.Host {
		case "", "0.0.0.0":
		case "::":
			host = "::1"
		default:
			host = cfg.Server.Host
		}
	}
	s.worker = reauthruntime.New(filepath.Join(dir, "credential-worker"), info.Version, "http://"+net.JoinHostPort(host, strconv.Itoa(port)), s.workerToken)
}

// WorkerAuthentication verifies the private protocol without exposing the key.
func (s *OpenAIOAuthReauthService) WorkerAuthentication(provided string) (configured, valid bool) {
	token := strings.TrimSpace(os.Getenv("OPENAI_REAUTH_WORKER_TOKEN"))
	if s != nil && s.workerToken != "" {
		token = s.workerToken
	}
	configured = len(token) >= 32
	return configured, configured && provided != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(token)) == 1
}

const workerHeartbeatTTL = 26 * time.Minute
const workerHeartbeatLimit = 1024

type workerHeartbeat struct {
	seenAt   int64
	isolated bool
}

func (s *OpenAIOAuthReauthService) WorkerStatus() reauthruntime.Status {
	if s == nil {
		return reauthruntime.Status{Mode: "managed", State: "unavailable", Reason: "worker_start_failed"}
	}
	now := time.Now()
	if s.worker != nil {
		status := s.worker.Status()
		isolated, unisolated := s.workerHeartbeatSummary(now, status.StartedAt)
		return managedWorkerHeartbeatStatus(status, isolated, unisolated, now)
	}
	configured, _ := s.WorkerAuthentication("")
	if !configured {
		return reauthruntime.Status{Mode: "external", State: "unavailable", Reason: "external_not_configured"}
	}
	isolated, unisolated := s.workerHeartbeatSummary(now, 0)
	if !OpenAIWarpEnabled() && (isolated || unisolated) {
		return reauthruntime.Status{Mode: "external", State: "running"}
	}
	if isolated {
		return reauthruntime.Status{Mode: "external", State: "running"}
	}
	if unisolated {
		return reauthruntime.Status{Mode: "external", State: "unavailable", Reason: "warp_isolation_required"}
	}
	return reauthruntime.Status{Mode: "external", State: "unavailable", Reason: "external_offline"}
}

func managedWorkerHeartbeatStatus(status reauthruntime.Status, isolated, unisolated bool, now time.Time) reauthruntime.Status {
	if !OpenAIWarpEnabled() {
		isolated = isolated || unisolated
		unisolated = false
	}
	if status.State == "running" && !isolated {
		status.State = "preparing"
		if unisolated {
			status.State = "unavailable"
			status.Reason = "warp_isolation_required"
		} else if now.Sub(time.Unix(0, status.StartedAt)) > time.Minute {
			status.State = "unavailable"
			status.Reason = "api_unreachable"
		}
	}
	return status
}

func (s *OpenAIOAuthReauthService) EnsureWorker() {
	if s != nil && s.worker != nil {
		s.worker.Ensure()
	}
}

// RecordWorkerHeartbeat stores an administrator declaration, not verification
// of network isolation. An unidentified or invalid worker can never be healthy.
func (s *OpenAIOAuthReauthService) RecordWorkerHeartbeat(workerID string, isolated bool) {
	if s == nil {
		return
	}
	s.recordWorkerHeartbeatAt(workerID, isolated, time.Time{})
}

func (s *OpenAIOAuthReauthService) recordWorkerHeartbeatAt(workerID string, isolated bool, now time.Time) {
	workerID = strings.TrimSpace(workerID)
	if len(workerID) > 128 || workerID == "" {
		workerID, isolated = "", false
	}
	s.workerHeartbeatMu.Lock()
	defer s.workerHeartbeatMu.Unlock()
	if now.IsZero() {
		// Read the clock under the lock so a delayed earlier caller cannot
		// overwrite a newer declaration with an older timestamp.
		now = time.Now()
	}
	if s.workerHeartbeats == nil {
		s.workerHeartbeats = make(map[string]workerHeartbeat)
	}
	cutoff := now.Add(-workerHeartbeatTTL).UnixNano()
	for id, entry := range s.workerHeartbeats {
		if entry.seenAt < cutoff {
			delete(s.workerHeartbeats, id)
		}
	}
	if _, exists := s.workerHeartbeats[workerID]; !exists && len(s.workerHeartbeats) >= workerHeartbeatLimit {
		// Unconfirmed workers cannot evict healthy workers from the bounded map.
		var oldestID string
		var oldest int64
		found := false
		for id, entry := range s.workerHeartbeats {
			if !entry.isolated && (!found || entry.seenAt < oldest) {
				oldestID, oldest, found = id, entry.seenAt, true
			}
		}
		if !found {
			return
		}
		delete(s.workerHeartbeats, oldestID)
	}
	// The mutex serializes updates, including those at the same clock tick.
	s.workerHeartbeats[workerID] = workerHeartbeat{seenAt: now.UnixNano(), isolated: isolated}
}

func (s *OpenAIOAuthReauthService) workerHeartbeatSummary(now time.Time, notBefore int64) (isolated, unisolated bool) {
	s.workerHeartbeatMu.Lock()
	defer s.workerHeartbeatMu.Unlock()
	cutoff := now.Add(-workerHeartbeatTTL).UnixNano()
	for id, entry := range s.workerHeartbeats {
		if entry.seenAt < cutoff {
			delete(s.workerHeartbeats, id)
			continue
		}
		if notBefore != 0 && entry.seenAt <= notBefore {
			continue
		}
		if entry.isolated {
			isolated = true
		} else {
			unisolated = true
		}
	}
	return isolated, unisolated
}

func (s *OpenAIOAuthReauthService) stopWorker() {
	if s != nil && s.worker != nil {
		s.worker.Stop()
	}
}

func (s *OpenAIOAuthReauthService) checkWorkerMode(mode string) error {
	// Constructors used by embedders/tests do not enable a runtime implicitly.
	if s.worker == nil {
		return nil
	}
	if mode != OpenAIOAuthReauthModePasswordTOTP {
		return infraerrors.ServiceUnavailable("OPENAI_REAUTH_MODE_UNAVAILABLE", "Email OTP re-login is unavailable in the built-in runtime")
	}
	s.EnsureWorker()
	if s.WorkerStatus().State == "unavailable" {
		return infraerrors.ServiceUnavailable("OPENAI_REAUTH_WORKER_UNAVAILABLE", "Re-login service is unavailable; see Credential Operations status")
	}
	return nil
}
