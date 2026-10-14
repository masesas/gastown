package cmd

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/refinery"
)

// bdCircuitOpenStderr is the exact stderr bd v1.3.0 prints when its
// cross-process circuit breaker rejects a connection (ErrCircuitOpen in
// beads internal/storage/dolt/circuit.go:91), captured from the pilot
// failure and from the reproduction against a real dolt sql-server.
const bdCircuitOpenStderr = "Error: failed to open database: dolt circuit breaker is open: server appears down, failing fast (cooldown 5s)"

var errBdCircuitOpen = errors.New("bd list --limit 1 --json: " + bdCircuitOpenStderr)

func TestIsBdCircuitOpen(t *testing.T) {
	if !isBdCircuitOpen(errBdCircuitOpen) {
		t.Errorf("real bd breaker error not recognized: %v", errBdCircuitOpen)
	}
	for _, err := range []error{nil, errors.New("bd list: unknown database 'x'"), beads.ErrNotFound, beads.ErrNotInstalled} {
		if isBdCircuitOpen(err) {
			t.Errorf("isBdCircuitOpen(%v) = true, want false", err)
		}
	}
}

func TestWaitForBdReady_WaitsOutCircuitBreaker(t *testing.T) {
	calls := 0
	probe := func(dir string, _ time.Duration) error {
		calls++
		if calls <= 3 {
			return errBdCircuitOpen
		}
		return nil
	}
	if err := waitForBdReady([]string{"/town/.beads"}, probe, 10*time.Second, time.Millisecond); err != nil {
		t.Fatalf("waitForBdReady = %v, want nil once the breaker closes", err)
	}
	if calls != 4 {
		t.Errorf("probe calls = %d, want 4 (3 breaker rejections + 1 success)", calls)
	}
}

func TestWaitForBdReady_TimesOutWhileBreakerStaysOpen(t *testing.T) {
	probe := func(dir string, _ time.Duration) error { return errBdCircuitOpen }
	err := waitForBdReady([]string{"/town/.beads"}, probe, 30*time.Millisecond, time.Millisecond)
	if err == nil {
		t.Fatal("waitForBdReady = nil, want timeout error while the breaker stays open")
	}
	if !strings.Contains(err.Error(), "/town/.beads") || !isBdCircuitOpen(err) {
		t.Errorf("timeout error must name the beads dir and keep the bd error: %v", err)
	}
}

// Only the breaker heals with time once Dolt accepts TCP. Any other result
// ends the wait at once: the probe must never become a retry loop that keeps
// feeding connection failures into bd's breaker, and real errors surface in
// the (fail-closed) operation that follows.
func TestWaitForBdReady_OtherResultsEndTheWaitImmediately(t *testing.T) {
	for _, result := range []error{nil, errors.New("bd list --limit 1 --json: unknown database 'hq'")} {
		calls := 0
		probe := func(dir string, _ time.Duration) error {
			calls++
			return result
		}
		if err := waitForBdReady([]string{"/town/.beads"}, probe, 10*time.Second, time.Millisecond); err != nil {
			t.Errorf("result %v: waitForBdReady = %v, want nil", result, err)
		}
		if calls != 1 {
			t.Errorf("result %v: probe calls = %d, want 1", result, calls)
		}
	}
}

func TestWaitForBdReady_ProbesEveryDatabase(t *testing.T) {
	rejected := map[string]int{}
	var probed []string
	probe := func(dir string, _ time.Duration) error {
		probed = append(probed, dir)
		if rejected[dir] < 2 {
			rejected[dir]++
			return errBdCircuitOpen
		}
		return nil
	}
	dirs := []string{"/town/.beads", "/town/rig/mayor/rig/.beads"}
	if err := waitForBdReady(dirs, probe, 10*time.Second, time.Millisecond); err != nil {
		t.Fatalf("waitForBdReady = %v", err)
	}
	for _, dir := range dirs {
		if rejected[dir] != 2 {
			t.Errorf("%s: breaker rejections seen = %d, want 2 (wait must cover every database)", dir, rejected[dir])
		}
	}
	if len(probed) != 6 {
		t.Errorf("probe calls = %d (%v), want 6", len(probed), probed)
	}
}

// The shared deadline must bound the whole wait: every probe is handed at
// most the time left, and databases not reached in time are reported.
func TestWaitForBdReady_ProbesAreBoundedByTheDeadline(t *testing.T) {
	const timeout = 40 * time.Millisecond
	var given []time.Duration
	probe := func(dir string, limit time.Duration) error {
		given = append(given, limit)
		return errBdCircuitOpen
	}
	err := waitForBdReady([]string{"/town/.beads", "/town/rig/.beads"}, probe, timeout, time.Millisecond)
	if err == nil {
		t.Fatal("waitForBdReady = nil, want deadline error")
	}
	if len(given) == 0 {
		t.Fatal("probe never ran")
	}
	for i, limit := range given {
		if limit <= 0 || limit > timeout {
			t.Errorf("probe %d got timeout %v, want within (0, %v]", i, limit, timeout)
		}
	}
	if !strings.Contains(err.Error(), "/town/rig/.beads") {
		t.Errorf("error must also name the database not reached before the deadline: %v", err)
	}
}

// probeBdOpen must kill a hung bd at its timeout instead of inheriting the
// 60s subprocess limit of beads.Run.
func TestProbeBdOpenKillsHungBd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script uses POSIX shell")
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beadsDir := filepath.Join(t.TempDir(), ".beads")
	writeServerMetadata(t, beadsDir)

	start := time.Now()
	err := probeBdOpen(beadsDir, 200*time.Millisecond)
	if err == nil {
		t.Fatal("probeBdOpen = nil for a hung bd, want an error")
	}
	if isBdCircuitOpen(err) {
		t.Fatalf("a killed bd must not look like the breaker (it would be retried): %v", err)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("probeBdOpen took %v, want it killed near its 200ms timeout", elapsed)
	}
}

func TestServerModeBeadsDirs(t *testing.T) {
	townRoot := t.TempDir()
	writeServerMetadata(t, filepath.Join(townRoot, ".beads"))
	writeServerMetadata(t, filepath.Join(townRoot, "alpha", "mayor", "rig", ".beads"))
	// beta is embedded mode: no Dolt server database to wait for.
	if err := os.MkdirAll(filepath.Join(townRoot, "beta", ".beads"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "rigs.json"), []byte(`{"rigs":{"alpha":{},"beta":{}}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := serverModeBeadsDirs(townRoot)
	want := []string{
		filepath.Join(townRoot, ".beads"),
		filepath.Join(townRoot, "alpha", "mayor", "rig", ".beads"),
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("serverModeBeadsDirs = %v, want %v", got, want)
	}
}

// Cold `gt up` (gt-pd1c.38): Dolt accepts TCP, but bd still fails fast because
// its shared circuit breaker was tripped while Dolt was down. The refinery
// safety check must run only after bd can open the database again.
func TestRefinerySafetyCheckWaitsForBdAfterColdStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake bd script uses POSIX shell")
	}
	townRoot := t.TempDir()
	writeServerMetadata(t, filepath.Join(townRoot, ".beads"))
	if err := os.MkdirAll(filepath.Join(townRoot, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(townRoot, "mayor", "town.json"), []byte(`{"name":"test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(townRoot, "testrig"), 0o755); err != nil {
		t.Fatal(err)
	}

	// The fake bd rejects the first 3 database commands with the breaker
	// error, like bd during its cooldown, then serves them normally.
	binDir := t.TempDir()
	logPath := filepath.Join(binDir, "bd.log")
	writeBreakerThenReadyBD(t, binDir, logPath, 3)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)

	// Without the wait, the check fails exactly like the pilot did.
	if _, err := refinery.ActiveSafetyStop(townRoot, "testrig"); err == nil || !isBdCircuitOpen(err) {
		t.Fatalf("safety check right after Dolt start: err = %v, want bd circuit breaker error", err)
	}

	if err := waitForBdReady(serverModeBeadsDirs(townRoot), probeBdOpen, 10*time.Second, 10*time.Millisecond); err != nil {
		t.Fatalf("waitForBdReady = %v, want nil after the breaker closes", err)
	}

	stop, err := refinery.ActiveSafetyStop(townRoot, "testrig")
	if err != nil {
		t.Fatalf("safety check after waitForBdReady: %v, want bd to open the database", err)
	}
	if stop != nil {
		t.Fatalf("safety stop = %+v, want none", stop)
	}

	logData, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logData)
	if !strings.Contains(log, "list") {
		t.Errorf("waitForBdReady did not probe bd with a database command; log:\n%s", log)
	}
}

func writeServerMetadata(t *testing.T, beadsDir string) {
	t.Helper()
	if err := os.MkdirAll(beadsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(beadsDir, "metadata.json"), []byte(`{"backend":"dolt","dolt_mode":"server"}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeBreakerThenReadyBD installs a fake bd whose first `rejections`
// database commands fail with bd's circuit-breaker error. It counts calls in
// a state file instead of using wall-clock time so the test is stable on a
// loaded machine.
func writeBreakerThenReadyBD(t *testing.T, binDir, logPath string, rejections int) {
	t.Helper()
	countPath := filepath.Join(binDir, "bd.count")
	script := fmt.Sprintf(`#!/bin/sh
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
printf '%%s\n' "$*" >> %q
n=$(cat %q 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > %q
if [ "$n" -le %d ]; then
  echo %q >&2
  exit 1
fi
echo '[]'
`, logPath, countPath, countPath, rejections, bdCircuitOpenStderr)
	if err := os.WriteFile(filepath.Join(binDir, "bd"), []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bd: %v", err)
	}
}
