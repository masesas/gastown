package polecat

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvRunID is the environment variable carrying the per-session run ID
// (generation). SessionManager.Start sets it on every new polecat session.
const EnvRunID = "GT_RUN"

// SessionHeartbeatStaleThreshold is the age at which a polecat session heartbeat
// is considered stale, indicating the agent process is likely dead.
// Configurable via operational.polecat.heartbeat_stale_threshold in settings/config.json.
const SessionHeartbeatStaleThreshold = 3 * time.Minute

// HeartbeatState represents the agent-reported state in a heartbeat v2 (gt-3vr5).
// Agents report their own state; the witness makes exactly one inference:
// "is the heartbeat fresh?" Everything else is agent-reported.
type HeartbeatState string

const (
	// HeartbeatWorking means the agent is actively processing.
	HeartbeatWorking HeartbeatState = "working"
	// HeartbeatIdle means the agent is waiting for input.
	HeartbeatIdle HeartbeatState = "idle"
	// HeartbeatExiting means the agent is in the gt done flow.
	HeartbeatExiting HeartbeatState = "exiting"
	// HeartbeatStuck means the agent self-reports being stuck.
	HeartbeatStuck HeartbeatState = "stuck"
)

// SessionHeartbeat represents a polecat session's heartbeat file.
// v1: timestamp only. v2 (gt-3vr5): adds agent-reported state, context, and bead.
type SessionHeartbeat struct {
	Timestamp time.Time      `json:"timestamp"`
	State     HeartbeatState `json:"state,omitempty"`   // v2: agent-reported state
	Context   string         `json:"context,omitempty"` // v2: what the agent is doing
	Bead      string         `json:"bead,omitempty"`    // v2: current hook bead ID
	// Generation is the GT_RUN of the session that wrote this heartbeat.
	// Empty for legacy writers and sessions started without GT_RUN.
	Generation string `json:"generation,omitempty"`
}

// HeartbeatMatchesRun reports whether hb belongs to the session run identified
// by run (the session's GT_RUN). Legacy heartbeats without a generation and
// sessions without GT_RUN are treated as matching, preserving pre-generation
// behavior.
func HeartbeatMatchesRun(hb *SessionHeartbeat, run string) bool {
	if hb == nil || hb.Generation == "" || run == "" {
		return true
	}
	return hb.Generation == run
}

// EffectiveState returns the agent-reported state, defaulting to HeartbeatWorking
// for v1 heartbeats without a state field (backwards compatibility). See gt-3vr5.
func (h *SessionHeartbeat) EffectiveState() HeartbeatState {
	if h.State == "" {
		return HeartbeatWorking
	}
	return h.State
}

// IsV2 returns true if this heartbeat carries a state field (heartbeat v2).
// Used by the witness to decide whether to use agent-reported state or fall
// through to legacy timer-based detection.
func (h *SessionHeartbeat) IsV2() bool {
	return h.State != ""
}

// heartbeatsDir returns the directory for polecat session heartbeat files.
// Heartbeats live under <townRoot>/.runtime/heartbeats/, parallel to .runtime/pids/.
func heartbeatsDir(townRoot string) string {
	return filepath.Join(townRoot, ".runtime", "heartbeats")
}

// heartbeatFile returns the path to a heartbeat file for a given session.
func heartbeatFile(townRoot, sessionName string) string {
	return filepath.Join(heartbeatsDir(townRoot), sessionName+".json")
}

// TouchSessionHeartbeat writes or updates the heartbeat file for a polecat session.
// Writes state="working" by default (heartbeat v2, gt-3vr5).
// This is best-effort: errors are silently ignored because heartbeat signals
// are non-critical and should not interrupt gt commands.
func TouchSessionHeartbeat(townRoot, sessionName string) {
	TouchSessionHeartbeatWithState(townRoot, sessionName, HeartbeatWorking, "", "")
}

// TouchSessionHeartbeatWithState writes a heartbeat with explicit state information.
// Used by gt done (state="exiting") and gt heartbeat (state="stuck"). See gt-3vr5.
// The generation is taken from the calling process's GT_RUN.
// This is best-effort: errors are silently ignored.
func TouchSessionHeartbeatWithState(townRoot, sessionName string, state HeartbeatState, context, bead string) {
	_ = WriteSessionHeartbeat(townRoot, sessionName, SessionHeartbeat{
		Timestamp:  time.Now().UTC(),
		State:      state,
		Context:    context,
		Bead:       bead,
		Generation: os.Getenv(EnvRunID),
	})
}

// WriteSessionHeartbeat atomically replaces the heartbeat file for a session.
// Used by SessionManager.Start to claim the heartbeat for a new generation
// before the runtime is ready, so a previous session's "exiting" heartbeat
// cannot be mistaken for the new session's state (#4890).
func WriteSessionHeartbeat(townRoot, sessionName string, hb SessionHeartbeat) error {
	dir := heartbeatsDir(townRoot)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating heartbeat dir: %w", err)
	}
	data, err := json.Marshal(hb)
	if err != nil {
		return fmt.Errorf("encoding heartbeat: %w", err)
	}
	tmp, err := os.CreateTemp(dir, sessionName+".*.tmp")
	if err != nil {
		return fmt.Errorf("creating heartbeat temp file: %w", err)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("writing heartbeat: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("closing heartbeat temp file: %w", err)
	}
	if err := os.Chmod(tmpName, 0644); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("setting heartbeat permissions: %w", err)
	}
	if err := os.Rename(tmpName, heartbeatFile(townRoot, sessionName)); err != nil {
		_ = os.Remove(tmpName)
		return fmt.Errorf("replacing heartbeat: %w", err)
	}
	return nil
}

// claimSessionHeartbeat writes a "working" heartbeat owned by runID.
// Best-effort: a failed write leaves the previous heartbeat in place, which the
// reaper then treats as another generation's.
func claimSessionHeartbeat(townRoot, sessionName, runID string) {
	_ = WriteSessionHeartbeat(townRoot, sessionName, SessionHeartbeat{
		Timestamp:  time.Now().UTC(),
		State:      HeartbeatWorking,
		Generation: runID,
	})
}

// ReadSessionHeartbeat reads the heartbeat for a polecat session.
// Returns nil if the file doesn't exist or can't be read.
// Callers that act destructively on the result should use ReadSessionHeartbeatE
// so an unreadable heartbeat is not mistaken for a missing one.
func ReadSessionHeartbeat(townRoot, sessionName string) *SessionHeartbeat {
	hb, err := ReadSessionHeartbeatE(townRoot, sessionName)
	if err != nil {
		return nil
	}
	return hb
}

// ReadSessionHeartbeatE reads the heartbeat for a polecat session.
// Returns (nil, nil) when no heartbeat file exists, and a non-nil error when
// the file exists but cannot be read or parsed (state unknown).
func ReadSessionHeartbeatE(townRoot, sessionName string) (*SessionHeartbeat, error) {
	data, err := os.ReadFile(heartbeatFile(townRoot, sessionName))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading heartbeat: %w", err)
	}

	var hb SessionHeartbeat
	if err := json.Unmarshal(data, &hb); err != nil {
		return nil, fmt.Errorf("parsing heartbeat: %w", err)
	}
	return &hb, nil
}

// IsSessionHeartbeatStale returns true if the session's heartbeat is older than
// the stale threshold, or if no heartbeat file exists.
//
// When no heartbeat file exists, this returns false to avoid false positives
// during the rollout period where sessions may not yet be touching heartbeats.
// The caller should fall back to other liveness checks in that case.
func IsSessionHeartbeatStale(townRoot, sessionName string) (stale bool, exists bool) {
	hb := ReadSessionHeartbeat(townRoot, sessionName)
	if hb == nil {
		return false, false
	}
	return time.Since(hb.Timestamp) >= SessionHeartbeatStaleThreshold, true
}

// RemoveSessionHeartbeat removes the heartbeat file for a session.
// Called during session cleanup.
func RemoveSessionHeartbeat(townRoot, sessionName string) {
	_ = os.Remove(heartbeatFile(townRoot, sessionName))
}
