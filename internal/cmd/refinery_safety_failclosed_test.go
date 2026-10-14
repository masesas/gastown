package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/refinery"
)

// bd 1.3.0 stderr when the Dolt server does not (yet) serve the database
// (gt-pd1c.39). Its "not found" used to read as "no refinery bead, no stop".
const cmdBdDatabaseMissingStderr = `Error: failed to open database: database "hq" not found on Dolt server at 127.0.0.1:3307

Tip: If this is an existing project, fresh clone, or shared-server recovery, run bd bootstrap first.`

// A refinery patrol must not be spawned when the safety stop cannot be read.
func TestAutoSpawnPatrol_RefinerySafetyCheckDatabaseMissingFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	townRoot := setupRefinerySafetyStopTown(t)
	logPath := installRefineryDBMissingMockBD(t)

	_, err := autoSpawnPatrol(PatrolConfig{
		RoleName:      "refinery",
		PatrolMolName: constants.MolRefineryPatrol,
		BeadsDir:      townRoot,
		Assignee:      "testrig/refinery",
	})
	if err == nil {
		t.Fatal("autoSpawnPatrol succeeded although the safety stop could not be read")
	}
	if errors.Is(err, refinery.ErrSafetyStopped) {
		t.Fatalf("autoSpawnPatrol error = %v, want the read failure, not ErrSafetyStopped", err)
	}
	if !strings.Contains(err.Error(), `database "hq" not found`) {
		t.Fatalf("autoSpawnPatrol error = %q, want the bd database error", err)
	}

	logData, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("read bd log: %v", readErr)
	}
	if strings.Contains(string(logData), "mol wisp create") || strings.Contains(string(logData), "update ") {
		t.Fatalf("autoSpawnPatrol mutated patrol state although the safety stop could not be read; log:\n%s", logData)
	}
}

func installRefineryDBMissingMockBD(t *testing.T) string {
	t.Helper()
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	stderrPath := filepath.Join(binDir, "show.stderr")
	if err := os.WriteFile(stderrPath, []byte(cmdBdDatabaseMissingStderr+"\n"), 0o644); err != nil {
		t.Fatalf("write fake bd stderr: %v", err)
	}
	script := `#!/bin/sh
printf '%s\n' "$*" >> "` + logPath + `"
cmd=""
for arg in "$@"; do
  case "$arg" in
    --*) ;;
    *) cmd="$arg"; break ;;
  esac
done
case "$cmd" in
  version)
    echo "bd test"
    ;;
  show)
    cat "` + stderrPath + `" >&2
    exit 1
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	return logPath
}
