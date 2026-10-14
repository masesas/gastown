package polecat

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// installHealthCheckMockBD installs a fake bd whose show fails with stderr and
// exit status 1, like bd 1.3.0.
func installHealthCheckMockBD(t *testing.T, stderr string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	binDir := t.TempDir()
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
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
}

// The health probe looks up a nonexistent ID: bd answering "Issue ... not
// found" proves the database responded.
func TestCheckDoltHealthMissingProbeIssueIsHealthy(t *testing.T) {
	installHealthCheckMockBD(t, "Issue __health_check_nonexistent__ not found\n"+
		"Hint: this ID may have never existed, or may reference a deleted/purged record with no trace left in the live database — try 'bd history __health_check_nonexistent__'")

	m := &Manager{beads: beads.New(t.TempDir())}
	if err := m.CheckDoltHealth(); err != nil {
		t.Fatalf("CheckDoltHealth = %v, want nil when bd reports the probe issue missing", err)
	}
}

// A database that the Dolt server does not serve also says "not found", but it
// is not a healthy answer: spawning must stop (gt-pd1c.39, gt-2ra). The raw
// Dolt wording is a config error and fails fast; bd's longer
// `database "hq" not found on Dolt server ...` wording is retried with backoff
// (Dolt warm-up) and then also ends in ErrDoltUnhealthy.
func TestCheckDoltHealthDatabaseMissingIsUnhealthy(t *testing.T) {
	installHealthCheckMockBD(t, `Error: failed to open database: database not found: hq`)

	m := &Manager{beads: beads.New(t.TempDir())}
	err := m.CheckDoltHealth()
	if !errors.Is(err, ErrDoltUnhealthy) {
		t.Fatalf("CheckDoltHealth = %v, want ErrDoltUnhealthy when the database is missing", err)
	}
	if !strings.Contains(err.Error(), "database not found: hq") {
		t.Fatalf("CheckDoltHealth = %q, want the bd error", err)
	}
}
