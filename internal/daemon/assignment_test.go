package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/polecat"
)

// writeFakeBDByStatus writes a bd script whose "list --status=<s>" result is
// controlled per status: "fail" exits 1, "bad" prints invalid JSON, anything
// else is printed as-is. "show" always fails (agent bead lookup degraded).
func writeFakeBDByStatus(t *testing.T, dir string, byStatus map[string]string) string {
	t.Helper()
	var cases strings.Builder
	for status, out := range byStatus {
		switch out {
		case "fail":
			fmt.Fprintf(&cases, "  *--status=%s*) exit 1;;\n", status)
		case "bad":
			fmt.Fprintf(&cases, "  *--status=%s*) echo 'not json'; exit 0;;\n", status)
		default:
			fmt.Fprintf(&cases, "  *--status=%s*) echo '%s'; exit 0;;\n", status, out)
		}
	}
	script := "#!/bin/sh\n" +
		"if [ \"$1\" != \"list\" ]; then exit 1; fi\n" +
		"case \"$*\" in\n" + cases.String() +
		"  *) echo '[]'; exit 0;;\n" +
		"esac\n"
	path := filepath.Join(dir, "bd")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("writing fake bd: %v", err)
	}
	return path
}

func TestAssignedOpenWork_TriState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses Unix shell script mocks")
	}
	work := `[{"id":"gt-1","status":"hooked"}]`
	tests := []struct {
		name     string
		byStatus map[string]string
		want     WorkAssignment
	}{
		{name: "work found", byStatus: map[string]string{"hooked": work}, want: WorkYes},
		{name: "no work anywhere", byStatus: map[string]string{}, want: WorkNo},
		{name: "every query fails", byStatus: map[string]string{"hooked": "fail", "in_progress": "fail", "open": "fail"}, want: WorkUnknown},
		{name: "one query fails, another finds work", byStatus: map[string]string{"hooked": "fail", "in_progress": work}, want: WorkYes},
		{name: "one query fails, others empty", byStatus: map[string]string{"open": "fail"}, want: WorkUnknown},
		{name: "unparseable output", byStatus: map[string]string{"in_progress": "bad"}, want: WorkUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bdPath := writeFakeBDByStatus(t, t.TempDir(), tt.byStatus)
			d := &Daemon{config: &Config{TownRoot: t.TempDir()}, bdPath: bdPath}
			if got := d.assignedOpenWork("myr", "myr/polecats/mycat"); got != tt.want {
				t.Errorf("assignedOpenWork() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWorkAssignmentZeroValueIsUnknown(t *testing.T) {
	var w WorkAssignment
	if w != WorkUnknown {
		t.Fatalf("zero WorkAssignment = %v, want WorkUnknown", w)
	}
}

func TestReapIdlePolecat_AssignedWorkUnknown_NotKilled(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir)
	bdPath := writeFakeBDByStatus(t, binDir, map[string]string{"hooked": "fail", "in_progress": "fail", "open": "fail"})
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)

	// Stale "working" heartbeat well past 3x timeout, dead agent, failing bd:
	// previously this was treated as "no work" and the session was killed.
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatWorking, ""))

	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)

	out := logBuf.String()
	if strings.Contains(out, "Reaping idle polecat") {
		t.Errorf("must not reap when assigned work is unknown, got: %q", out)
	}
	if !strings.Contains(out, "state unknown") {
		t.Errorf("expected a state unknown log line, got: %q", out)
	}
}

func TestReapIdlePolecat_AssignedWorkUnknown_EscalatesOncePerEpisode(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir)
	bdPath := writeFakeBDByStatus(t, binDir, map[string]string{"hooked": "fail", "in_progress": "fail", "open": "fail"})
	d, logBuf := newReapTestDaemon(t, binDir, bdPath)
	gtPath, gtLog := writeFakeGT(t, binDir, 0)
	d.gtPath = gtPath
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatWorking, ""))

	for i := 0; i < unknownEscalationCycles+2; i++ {
		d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	}

	if got := countLines(t, gtLog); got != 1 {
		data, _ := os.ReadFile(gtLog)
		t.Errorf("witness mails = %d, want exactly 1; gt log:\n%s", got, data)
	}
	if got := strings.Count(logBuf.String(), "escalating"); got != 1 {
		t.Errorf("escalations = %d, want exactly 1; log: %q", got, logBuf.String())
	}
	if strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("must never reap while state is unknown, got: %q", logBuf.String())
	}
}

// writeFakeGT writes a gt script that logs each invocation and fails the
// first failFirst calls.
func writeFakeGT(t *testing.T, dir string, failFirst int) (path, logPath string) {
	t.Helper()
	logPath = filepath.Join(dir, "gt.log")
	script := fmt.Sprintf("#!/bin/sh\n"+
		"echo call >> %q\n"+
		"n=$(wc -l < %q)\n"+
		"if [ \"$n\" -le %d ]; then exit 1; fi\n"+
		"exit 0\n", logPath, logPath, failFirst)
	path = filepath.Join(dir, "gt")
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("writing fake gt: %v", err)
	}
	return path, logPath
}

func countLines(t *testing.T, path string) int {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(data), "\n")
}

func TestReapIdlePolecat_UnknownEscalation_RetriesFailedNotification(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir)
	bdPath := writeFakeBDByStatus(t, binDir, map[string]string{"hooked": "fail", "in_progress": "fail", "open": "fail"})
	d, _ := newReapTestDaemon(t, binDir, bdPath)
	gtPath, gtLog := writeFakeGT(t, binDir, 1)
	d.gtPath = gtPath
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatWorking, ""))

	for i := 0; i < unknownEscalationCycles+2; i++ {
		d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	}

	// Cycle 3 escalates and the mail fails; cycle 4 retries and succeeds;
	// cycle 5 does not send again.
	if got := countLines(t, gtLog); got != 2 {
		t.Errorf("gt mail invocations = %d, want 2 (failed + retry)", got)
	}
}

func TestReapIdlePolecat_FreshHeartbeatEndsUnknownEpisode(t *testing.T) {
	binDir := t.TempDir()
	writeFakeTmuxIdleSession(t, binDir)
	bdPath := writeFakeBDByStatus(t, binDir, map[string]string{"hooked": "fail", "in_progress": "fail", "open": "fail"})
	d, _ := newReapTestDaemon(t, binDir, bdPath)
	gtPath, gtLog := writeFakeGT(t, binDir, 0)
	d.gtPath = gtPath

	stale := heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatWorking, "")
	for i := 0; i < unknownEscalationCycles-1; i++ {
		writeReapTestHeartbeat(t, d.config.TownRoot, stale)
		d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	}
	// The polecat becomes active again in between.
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 0, polecat.HeartbeatWorking, ""))
	d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	for i := 0; i < unknownEscalationCycles-1; i++ {
		writeReapTestHeartbeat(t, d.config.TownRoot, stale)
		d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	}

	if got := countLines(t, gtLog); got != 0 {
		t.Errorf("non-consecutive unknown cycles must not escalate, got %d mails", got)
	}
}
