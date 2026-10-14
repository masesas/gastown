package refinery

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/rig"
)

// bdDatabaseMissingStderr is the stderr bd 1.3.0 prints when the Dolt server
// does not (yet) serve the town database, captured from a real bd against a
// dolt sql-server (gt-pd1c.39). Before the fix its "not found" made the
// safety-stop check read "no refinery bead" and start the refinery.
const bdDatabaseMissingStderr = `Error: failed to open database: database "hq" not found on Dolt server at 127.0.0.1:3307

Common causes:
  - Switched git branches (the Dolt database is runtime state, not in git)

Tip: If this is an existing project, fresh clone, or shared-server recovery, run bd bootstrap first.`

const bdCircuitOpenStderr = "Error: failed to open database: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"

// bdRefineryMissingStderr is what bd 1.3.0 prints for a refinery bead that
// genuinely does not exist.
const bdRefineryMissingStderr = `Issue gt-testrig-refinery not found
Hint: this ID may have never existed, or may reference a deleted/purged record with no trace left in the live database — try 'bd history gt-testrig-refinery'`

func setupSafetyStopTown(t *testing.T) string {
	t.Helper()
	townRoot := t.TempDir()
	for _, dir := range []string{filepath.Join(townRoot, "mayor"), filepath.Join(townRoot, ".beads"), filepath.Join(townRoot, "testrig")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}
	return townRoot
}

// If bd cannot read the refinery agent bead when the refinery starts (bd's
// circuit breaker open after a cold Dolt start, gt-pd1c.38; or the Dolt server
// not serving the database yet, gt-pd1c.39), the safety-stop check cannot
// prove the refinery is allowed to run. Start must fail closed with a clear
// error and must not create a tmux session.
func TestManager_StartFailsClosedWhenSafetyCheckCannotReadBeads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd/tmux scripts use POSIX shell")
	}
	for _, tt := range []struct {
		name   string
		stderr string
		want   string
	}{
		{"circuit breaker open", bdCircuitOpenStderr, "circuit breaker is open"},
		{"database not found", bdDatabaseMissingStderr, `database "hq" not found`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			setupTestRegistry(t)
			townRoot := setupSafetyStopTown(t)

			binDir := t.TempDir()
			logPath := filepath.Join(t.TempDir(), "commands.log")
			writeFailingShowMockBD(t, binDir, tt.stderr)
			writeSafetyStopMockTmux(t, binDir, logPath)
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			beads.ResetBdAllowStaleCacheForTest()
			t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

			mgr := NewManager(&rig.Rig{Name: "testrig", Path: filepath.Join(townRoot, "testrig")})
			err := mgr.Start(false, "")
			if err == nil {
				t.Fatal("Start succeeded although the safety-stop check could not read beads")
			}
			if errors.Is(err, ErrSafetyStopped) || errors.Is(err, ErrAlreadyRunning) {
				t.Fatalf("Start error = %v, want the safety-check failure itself", err)
			}
			for _, want := range []string{"checking refinery safety stop", tt.want} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("Start error %q does not mention %q", err, want)
				}
			}
			logData, readErr := os.ReadFile(logPath)
			if readErr != nil && !os.IsNotExist(readErr) {
				t.Fatalf("read command log: %v", readErr)
			}
			if strings.Contains(string(logData), "new-session") {
				t.Fatalf("Start created a tmux session although the safety check failed; log:\n%s", logData)
			}
		})
	}
}

func TestActiveSafetyStopReturnsErrorWhenDatabaseMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	townRoot := setupSafetyStopTown(t)
	binDir := t.TempDir()
	writeFailingShowMockBD(t, binDir, bdDatabaseMissingStderr)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	stop, err := ActiveSafetyStop(townRoot, "testrig")
	if err == nil {
		t.Fatalf("ActiveSafetyStop = (%v, nil), want an error when bd cannot open the database", stop)
	}
	if errors.Is(err, beads.ErrNotFound) {
		t.Fatalf("ActiveSafetyStop error = %v, want a database error, not ErrNotFound", err)
	}
	for _, want := range []string{"gt-testrig-refinery", `database "hq" not found`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("ActiveSafetyStop error %q does not mention %q", err, want)
		}
	}
}

func TestActiveSafetyStopMissingRefineryBeadIsNoStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	townRoot := setupSafetyStopTown(t)
	binDir := t.TempDir()
	writeFailingShowMockBD(t, binDir, bdRefineryMissingStderr)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	stop, err := ActiveSafetyStop(townRoot, "testrig")
	if err != nil {
		t.Fatalf("ActiveSafetyStop for a missing refinery bead returned error %v, want (nil, nil)", err)
	}
	if stop != nil {
		t.Fatalf("ActiveSafetyStop for a missing refinery bead = %+v, want nil", stop)
	}
}

// writeFailingShowMockBD installs a fake bd whose show fails with stderr and
// exit status 1, like bd 1.3.0.
func writeFailingShowMockBD(t *testing.T, binDir, stderr string) {
	t.Helper()
	stderrPath := filepath.Join(binDir, "show.stderr")
	if err := os.WriteFile(stderrPath, []byte(stderr+"\n"), 0o644); err != nil {
		t.Fatalf("write fake bd stderr: %v", err)
	}
	script := `#!/bin/sh
cmd=""
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) cmd="$arg"; break ;;
  esac
done
if [ "$cmd" = "version" ]; then
  echo "bd test"
  exit 0
fi
cat "` + stderrPath + `" >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}
