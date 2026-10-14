package beads

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
)

// Real stderr captured from bd 1.3.0 (gt-pd1c.39). The missing-issue samples
// come from running bd against an embedded workspace; the database sample from
// a bd workspace pointed at a dolt sql-server that does not serve the
// configured database.
const (
	bdShowMissingStderr = "Issue zz-nope not found\n" +
		"Hint: this ID may have never existed, or may reference a deleted/purged record with no trace left in the live database — try 'bd history zz-nope'"
	bdUpdateMissingStderr = "Error resolving zz-nope: no issue found matching \"zz-nope\"\n" +
		"Error: 1 of 1 issues failed to update\n" +
		"  zz-nope: resolving issue: no issue found matching \"zz-nope\""
	bdCloseMissingStderr    = "Error: resolving ID zz-nope: no issue found matching \"zz-nope\""
	bdLabelAddMissingStderr = "Error: resolving issue ID \"zz-nope\": no issue found matching \"zz-nope\""
	bdDepAddMissingStderr   = "Error: resolving issue ID zz-nope: no issue found matching \"zz-nope\""
	bdDatabaseMissingStderr = "Error: failed to open database: database \"hq\" not found on Dolt server at 127.0.0.1:3307\n\n" +
		"Common causes:\n" +
		"  - Switched git branches (the Dolt database is runtime state, not in git)\n" +
		"  - The server is serving a different data directory than expected\n\n" +
		"To diagnose:\n" +
		"  bd doctor                  # Check server and database health\n\n" +
		"Tip: If this is an existing project, fresh clone, or shared-server recovery, run bd bootstrap first."
	bdCircuitOpenStderr = "Error: failed to open database: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"
)

func TestWrapErrorClassifiesOnlyMissingIssuesAsNotFound(t *testing.T) {
	b := &Beads{}
	notFound := []struct {
		name   string
		stderr string
	}{
		{"show", bdShowMissingStderr},
		{"update", bdUpdateMissingStderr},
		{"close", bdCloseMissingStderr},
		{"label add", bdLabelAddMissingStderr},
		{"dep add", bdDepAddMissingStderr},
		{"delete / comments add", "Error: issue gt-xyz not found"},
		{"proxied mutate", "issue gt-xyz not found"},
		{"wrapped storage sentinel", "Error fetching gt-xyz: not found: issue gt-xyz"},
		{"update failure summary", "Error: 1 of 1 issues failed to update\n  gt-xyz: issue not found"},
		{"legacy show text", "Issue not found: gt-xyz"},
		{"lowercase legacy text", "Error: issue not found: gt-xyz"},
		{"quoted id", "Error: issue 'gt-xyz' not found"},
		{"batch delete", "Error: issues not found: gt-xyz, gt-abc.1"},
		{"prefixed per-id close line", "Error closing gt-xyz: issue gt-xyz not found"},
		{"hyphenless probe id", "Issue __health_check_nonexistent__ not found"},
		{"warning line and CRLF", "Warning: beads.role not configured\r\nIssue gt-xyz not found\r\nHint: try 'bd history gt-xyz'"},
	}
	for _, tt := range notFound {
		t.Run("not_found/"+tt.name, func(t *testing.T) {
			err := b.wrapError(errors.New("exit status 1"), tt.stderr, []string{"show", "gt-xyz", "--json"})
			if err != ErrNotFound { //nolint:errorlint // callers compare the sentinel with ==
				t.Fatalf("wrapError(%q) = %v, want the bare ErrNotFound sentinel", tt.stderr, err)
			}
		})
	}

	other := []struct {
		name   string
		stderr string
		want   string
	}{
		{"database not found", bdDatabaseMissingStderr, `database "hq" not found on Dolt server`},
		{"raw dolt database not found", "Error: failed to open database: database not found: hq", "database not found: hq"},
		{"table not found", "Error 1146: table not found: issues", "table not found: issues"},
		{"show fetch hits missing table", "Error fetching gt-xyz: failed to search issues: table not found: issues", "table not found: issues"},
		{"circuit breaker", bdCircuitOpenStderr, "circuit breaker is open"},
		{"command not found", "sh: bd-helper: command not found", "command not found"},
		{"merge slot not found", "Error: merge slot not found: gt-merge-slot (run 'bd merge-slot create' first)", "merge slot not found"},
		{"molecule not found", "Error: molecule 'mol-x' not found", "molecule 'mol-x' not found"},
		{"formula not found", "Error: formula \"mol-x\" not found in search paths", "not found in search paths"},
		{"dolt directory not found", "Error: Dolt directory not found at /tmp/x/.beads/dolt", "Dolt directory not found"},
		{"beads directory not found", "Error: not a beads workspace: .beads directory not found for /tmp/x", ".beads directory not found"},
		{"parent filter not found", "Error: parent issue 'gt-epic' not found", "parent issue 'gt-epic' not found"},
		{"bare id not found", "gt-xyz not found", "gt-xyz not found"},
		{"bare not found", "not found", "not found"},
		{"multi-word issue lookalike", "Issue not found in /tmp/town/.beads", "Issue not found in"},
		{"show json summary", "no issues found matching the provided IDs", "no issues found matching"},
		{"quoted id with wrapped cause", "Error: issue 'gt-xyz' not found: table not found: issues", "table not found: issues"},
		{"quoted parent issue", "Error: issue 'gt-epic' not found as parent", "not found as parent"},
	}
	for _, tt := range other {
		t.Run("error/"+tt.name, func(t *testing.T) {
			err := b.wrapError(errors.New("exit status 1"), tt.stderr, []string{"show", "gt-xyz", "--json"})
			if err == nil {
				t.Fatal("wrapError returned nil")
			}
			if errors.Is(err, ErrNotFound) {
				t.Fatalf("wrapError(%q) = ErrNotFound, want an ordinary error carrying the stderr", tt.stderr)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("wrapError(%q) = %q, want it to carry %q", tt.stderr, err, tt.want)
			}
			if !strings.Contains(err.Error(), "bd show gt-xyz --json") {
				t.Fatalf("wrapError(%q) = %q, want the bd command for context", tt.stderr, err)
			}
		})
	}
}

// bd can exit 0 with empty stdout and the message on stderr; run() sends that
// through wrapError too.
func TestShowExitZeroWithStderrOnlyIsClassified(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	for _, tt := range []struct {
		name         string
		stderr       string
		wantNotFound bool
	}{
		{"missing issue", bdShowMissingStderr, true},
		{"database missing", bdDatabaseMissingStderr, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			stderrPath := filepath.Join(binDir, "show.stderr")
			if err := os.WriteFile(stderrPath, []byte(tt.stderr+"\n"), 0o644); err != nil {
				t.Fatalf("write stderr: %v", err)
			}
			script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = version ] && { echo bd test; exit 0; }; done\ncat \"" + stderrPath + "\" >&2\nexit 0\n"
			if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
				t.Fatalf("write mock bd: %v", err)
			}
			t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
			ResetBdAllowStaleCacheForTest()
			t.Cleanup(ResetBdAllowStaleCacheForTest)

			_, err := New(t.TempDir()).Show("gt-xyz")
			if err == nil {
				t.Fatal("Show returned nil error")
			}
			if got := errors.Is(err, ErrNotFound); got != tt.wantNotFound {
				t.Fatalf("Show error = %v, ErrNotFound = %v, want %v", err, got, tt.wantNotFound)
			}
		})
	}
}

func TestWrapErrorMapsMissingBinaryToNotInstalled(t *testing.T) {
	b := &Beads{}
	err := b.wrapError(&exec.Error{Name: "bd", Err: exec.ErrNotFound}, "", []string{"show", "gt-xyz"})
	if !errors.Is(err, ErrNotInstalled) {
		t.Fatalf("wrapError(exec.ErrNotFound) = %v, want ErrNotInstalled", err)
	}
}

// installMockBDShowFailure installs a fake bd whose show subcommand fails with
// the given stderr and exit status 1.
func installMockBDShowFailure(t *testing.T, stderr string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock bd script uses POSIX shell")
	}
	binDir := t.TempDir()
	script := `#!/bin/sh
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
    exit 0
    ;;
  show)
    printf '%s\n' "$MOCK_BD_SHOW_STDERR" >&2
    exit 1
    ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write mock bd: %v", err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("MOCK_BD_SHOW_STDERR", stderr)
	ResetBdAllowStaleCacheForTest()
	t.Cleanup(ResetBdAllowStaleCacheForTest)
}

func TestGetAgentBeadMissingBeadReturnsNil(t *testing.T) {
	installMockBDShowFailure(t, bdShowMissingStderr)

	issue, fields, err := New(t.TempDir()).GetAgentBead("gt-testrig-refinery")
	if err != nil {
		t.Fatalf("GetAgentBead for a missing bead returned error %v, want nil", err)
	}
	if issue != nil || fields != nil {
		t.Fatalf("GetAgentBead for a missing bead = (%v, %v), want (nil, nil)", issue, fields)
	}
}

func TestGetAgentBeadDatabaseErrorIsReturned(t *testing.T) {
	installMockBDShowFailure(t, bdDatabaseMissingStderr)

	issue, _, err := New(t.TempDir()).GetAgentBead("gt-testrig-refinery")
	if err == nil {
		t.Fatalf("GetAgentBead returned (%v, nil) although bd could not open the database", issue)
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("GetAgentBead error = %v, want a database error, not ErrNotFound", err)
	}
	if !strings.Contains(err.Error(), `database "hq" not found`) {
		t.Fatalf("GetAgentBead error = %q, want the bd stderr", err)
	}
}

func TestStoreShowOnlyMapsSDKNotFoundToErrNotFound(t *testing.T) {
	store := newMockStorage()
	store.getErr = fmt.Errorf("get issue: %w", errors.New(`database "hq" not found`))
	b := newTestBeads(store)

	_, err := b.Show("gt-xyz")
	if err == nil {
		t.Fatal("Show returned nil error for a store failure")
	}
	if errors.Is(err, ErrNotFound) {
		t.Fatalf("Show error = %v, want a store error, not ErrNotFound", err)
	}

	store.getErr = fmt.Errorf("%w: issue %s", beadsdk.ErrNotFound, "gt-xyz")
	if _, err := b.Show("gt-xyz"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Show error = %v, want ErrNotFound for the SDK not-found sentinel", err)
	}
}
