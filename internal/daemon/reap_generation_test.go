package daemon

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/polecat"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/tmux"
)

const reapTestTimeout = 15 * time.Minute

// writeFakeTmuxWithRun simulates a live session whose environment carries
// GT_RUN=run. paneCommand controls agent liveness (e.g., "claude" or "bash").
func writeFakeTmuxWithRun(t *testing.T, dir, paneCommand, run string) {
	t.Helper()
	script := fmt.Sprintf("#!/bin/sh\n"+
		"case \"$*\" in\n"+
		"  *has-session*) exit 0;;\n"+
		"  *show-environment*) echo 'GT_RUN=%s';;\n"+
		"  *display-message*) echo '%s';;\n"+
		"  *kill-session*) exit 0;;\n"+
		"  *) exit 1;;\n"+
		"esac\n", run, paneCommand)
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0755); err != nil {
		t.Fatalf("writing fake tmux: %v", err)
	}
}

// newReapTestDaemon wires a daemon against fake tmux/bd binaries in binDir.
func newReapTestDaemon(t *testing.T, binDir, bdPath string) (*Daemon, *strings.Builder) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell script mocks for tmux and bd")
	}
	old := session.DefaultRegistry()
	reg := session.NewPrefixRegistry()
	reg.Register("myr", "myr")
	session.SetDefaultRegistry(reg)
	t.Cleanup(func() { session.SetDefaultRegistry(old) })

	t.Setenv("PATH", binDir+":"+os.Getenv("PATH"))

	var logBuf strings.Builder
	d := &Daemon{
		config: &Config{TownRoot: t.TempDir()},
		logger: log.New(&logBuf, "", 0),
		tmux:   tmux.NewTmuxWithSocket(""),
		bdPath: bdPath,
	}
	return d, &logBuf
}

func writeReapTestHeartbeat(t *testing.T, townRoot string, raw []byte) {
	t.Helper()
	path := filepath.Join(townRoot, ".runtime", "heartbeats", "myr-mycat.json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0644); err != nil {
		t.Fatal(err)
	}
}

func heartbeatJSON(t *testing.T, age time.Duration, state polecat.HeartbeatState, generation string) []byte {
	t.Helper()
	data, err := json.Marshal(polecat.SessionHeartbeat{
		Timestamp:  time.Now().UTC().Add(-age),
		State:      state,
		Generation: generation,
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestReapIdlePolecat_OtherGenerationExiting_NotKilled(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxWithRun(t, binDir, "bash", "run-new")
	bdPath := writeFakeBDLookupFail(t, binDir, false)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	// Previous session left an exiting heartbeat; the new session has not
	// written one yet (#4890).
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 2*reapTestTimeout, polecat.HeartbeatExiting, "run-old"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	if strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("must not reap a new session based on the previous session's heartbeat, got: %q", logBuf.String())
	}
}

func TestReapIdlePolecat_SameGenerationExiting_Killed(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxWithRun(t, binDir, "bash", "run-new")
	bdPath := writeFakeBDLookupFail(t, binDir, false)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 2*reapTestTimeout, polecat.HeartbeatExiting, "run-new"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	if !strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("expected exiting session of the same generation to be reaped, got: %q", logBuf.String())
	}
}

func TestReapIdlePolecat_OtherGenerationVeryOld_UsesWorkingChecks(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxWithRun(t, binDir, "bash", "run-new")
	bdPath := writeFakeBDLookupFail(t, binDir, false /* no work */)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	// New session never wrote a heartbeat: after 3x timeout it is evaluated
	// with the "working" checks (assigned work + agent liveness).
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatExiting, "run-old"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	if !strings.Contains(logBuf.String(), "working-bead-lookup-failed") {
		t.Errorf("expected orphaned session to go through the working checks, got: %q", logBuf.String())
	}
}

func TestReapIdlePolecat_OtherGenerationVeryOldWithWork_NotKilled(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxWithRun(t, binDir, "bash", "run-new")
	bdPath := writeFakeBDLookupFail(t, binDir, true /* has work */)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatExiting, "run-old"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	if strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("must not reap a session with assigned open work, got: %q", logBuf.String())
	}
}

func TestReapIdlePolecat_CorruptHeartbeat_NotKilled(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxWithRun(t, binDir, "bash", "run-new")
	bdPath := writeFakeBDLookupFail(t, binDir, false)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	writeReapTestHeartbeat(t, d.config.TownRoot, []byte("{truncated"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	out := logBuf.String()
	if strings.Contains(out, "Reaping idle polecat") {
		t.Errorf("must not reap when the heartbeat cannot be read, got: %q", out)
	}
	if !strings.Contains(out, "state unknown") {
		t.Errorf("expected a state unknown log line, got: %q", out)
	}
}

func TestReapIdlePolecat_SessionRunLookupFails_NotKilled(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir) // show-environment fails
	bdPath := writeFakeBDLookupFail(t, binDir, false)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 2*reapTestTimeout, polecat.HeartbeatExiting, "run-old"))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	out := logBuf.String()
	if strings.Contains(out, "Reaping idle polecat") {
		t.Errorf("must not reap when the session generation cannot be read, got: %q", out)
	}
	if !strings.Contains(out, "state unknown") {
		t.Errorf("expected a state unknown log line, got: %q", out)
	}
}

func TestReapIdlePolecat_LegacyExitingHeartbeat_StillReaped(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir)
	bdPath := writeFakeBDLookupFail(t, binDir, false)
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	// Heartbeat written by a pre-generation binary: behavior is unchanged.
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 2*reapTestTimeout, polecat.HeartbeatExiting, ""))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	if !strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("legacy exiting heartbeat should still be reaped, got: %q", logBuf.String())
	}
}
