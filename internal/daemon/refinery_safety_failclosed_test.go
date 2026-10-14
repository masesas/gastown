package daemon

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/tmux"
)

// bd 1.3.0 stderr when the Dolt server does not (yet) serve the database
// (gt-pd1c.39). It contains "not found", which used to be read as "the
// refinery bead does not exist, so there is no safety stop".
const daemonBdDatabaseMissingStderr = `Error: failed to open database: database "hq" not found on Dolt server at 127.0.0.1:3307

Tip: If this is an existing project, fresh clone, or shared-server recovery, run bd bootstrap first.`

// When the refinery agent bead cannot be read, the daemon must not auto-start
// the refinery: it logs the failure and retries on the next heartbeat.
func TestEnsureRefineryRunningDatabaseMissingDoesNotSpawn(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd/tmux scripts use POSIX shell")
	}
	townRoot := t.TempDir()
	writeDaemonTownFile(t, townRoot, "mayor/town.json", `{"name":"test"}`)
	writeDaemonTownFile(t, townRoot, ".beads/metadata.json", `{"prefix":"hq"}`)
	writeDaemonTownFile(t, townRoot, "events/refinery/pending.event", "{}")
	if err := os.MkdirAll(filepath.Join(townRoot, "testrig"), 0o755); err != nil {
		t.Fatalf("mkdir rig: %v", err)
	}

	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "commands.log")
	writeDaemonRefineryDBMissingMockBD(t, binDir, logPath)
	writeDaemonSafetyStopMockTmux(t, binDir, logPath)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	var logBuf bytes.Buffer
	d := &Daemon{
		config: DefaultConfig(townRoot),
		logger: log.New(&logBuf, "", 0),
		tmux:   tmux.NewTmux(),
	}
	d.ensureRefineryRunning("testrig")

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read command log: %v", err)
	}
	if !strings.Contains(string(logData), "testrig-refinery") {
		t.Fatalf("daemon never looked up the refinery bead; log:\n%s", logData)
	}
	if strings.Contains(string(logData), "new-session") {
		t.Fatalf("daemon spawned the refinery although its safety stop could not be read; log:\n%s", logData)
	}
	for _, want := range []string{"cannot verify safety stop", `database "hq" not found`} {
		if !strings.Contains(logBuf.String(), want) {
			t.Errorf("daemon log = %q, want it to mention %q", logBuf.String(), want)
		}
	}
}

// writeDaemonRefineryDBMissingMockBD answers the rig bead lookup (so the rig
// is operational) but fails the refinery bead lookup with a database error.
func writeDaemonRefineryDBMissingMockBD(t *testing.T, binDir, logPath string) {
	t.Helper()
	stderrPath := filepath.Join(binDir, "refinery.stderr")
	if err := os.WriteFile(stderrPath, []byte(daemonBdDatabaseMissingStderr+"\n"), 0o644); err != nil {
		t.Fatalf("write fake bd stderr: %v", err)
	}
	script := `#!/bin/sh
printf 'bd %s\n' "$*" >> "` + logPath + `"
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
    case "$*" in
      *-rig-testrig*)
        printf '%s\n' '[{"id":"gt-rig-testrig","title":"Rig","issue_type":"task","labels":[],"status":"open","description":""}]'
        ;;
      *)
        cat "` + stderrPath + `" >&2
        exit 1
        ;;
    esac
    ;;
  *)
    exit 0
    ;;
esac
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}
