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

// If bd still cannot open the database when the refinery starts (gt-pd1c.38:
// bd's circuit breaker open after a cold Dolt start), the safety-stop check
// cannot prove the refinery is allowed to run. Start must fail closed with a
// clear error and must not create a tmux session.
func TestManager_StartFailsClosedWhenSafetyCheckCannotReadBeads(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd/tmux scripts use POSIX shell")
	}
	setupTestRegistry(t)

	townRoot := t.TempDir()
	for _, dir := range []string{filepath.Join(townRoot, "mayor"), filepath.Join(townRoot, ".beads"), filepath.Join(townRoot, "testrig")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "commands.log")
	writeCircuitOpenMockBD(t, binDir)
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
	for _, want := range []string{"checking refinery safety stop", "circuit breaker is open"} {
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
}

func writeCircuitOpenMockBD(t *testing.T, binDir string) {
	t.Helper()
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
echo "Error: failed to open database: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)" >&2
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}
