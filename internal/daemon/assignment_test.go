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
	writeReapTestHeartbeat(t, d.config.TownRoot, heartbeatJSON(t, 4*reapTestTimeout, polecat.HeartbeatWorking, ""))

	for i := 0; i < unknownEscalationCycles+2; i++ {
		d.reapIdlePolecat("myr", "mycat", reapTestTimeout)
	}

	if got := strings.Count(logBuf.String(), "escalating"); got != 1 {
		t.Errorf("escalations = %d, want exactly 1; log: %q", got, logBuf.String())
	}
	if strings.Contains(logBuf.String(), "Reaping idle polecat") {
		t.Errorf("must never reap while state is unknown, got: %q", logBuf.String())
	}
}
