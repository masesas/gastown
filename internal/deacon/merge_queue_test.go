package deacon

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
)

// installDeaconBDStub installs a bd stub answering `list --status=hooked` with
// hookedJSON and `show` with showJSON, logging every call.
func installDeaconBDStub(t *testing.T, hookedJSON, showJSON string) (townRoot, logPath string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("bd stub is a shell script")
	}
	beads.ResetBdAllowStaleCacheForTest()
	t.Cleanup(beads.ResetBdAllowStaleCacheForTest)
	townRoot = t.TempDir()
	if err := os.MkdirAll(filepath.Join(townRoot, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	logPath = filepath.Join(bin, "calls.log")
	for name, body := range map[string]string{"hooked.json": hookedJSON, "show.json": showJSON} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	script := `#!/bin/sh
if [ "${1:-}" = "--allow-stale" ]; then
  if [ "${2:-}" = "version" ]; then echo "Error: unknown flag: --allow-stale" >&2; exit 0; fi
  shift
fi
echo "$*" >> "` + logPath + `"
case "${1:-}" in
  list) cat "` + bin + `/hooked.json" ;;
  show) cat "` + bin + `/show.json" ;;
  *) printf '%s\n' '[]' ;;
esac
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "bd"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return townRoot, logPath
}

func stubDeaconMergeQueue(t *testing.T, idx beads.MergeQueueIndex, err error) {
	t.Helper()
	old := mergeQueueFn
	mergeQueueFn = func(string) *beads.MergeQueue { return beads.NewMergeQueue(idx, err, nil) }
	t.Cleanup(func() { mergeQueueFn = old })
}

func deaconCalls(t *testing.T, logPath string) string {
	t.Helper()
	data, _ := os.ReadFile(logPath)
	return string(data)
}

func pendingIndex(source string) beads.MergeQueueIndex {
	return beads.MergeQueueIndex{source: {{ID: "gt-wisp-mr1", Status: "open", Description: "source_issue: " + source + "\n"}}}
}

// An unassigned hooked bead older than MaxAge is stale. With a pending MR the
// deacon parks it for the refinery instead of reopening it (gt-pd1c.59).
const staleHookedJSON = `[{"id":"gt-work1","title":"Work","status":"hooked","assignee":"","updated_at":"2020-01-01T00:00:00Z"}]`

func TestScanStaleHooks_ParksBeadHeldByPendingMR(t *testing.T) {
	townRoot, logPath := installDeaconBDStub(t, staleHookedJSON, `[{"id":"gt-work1","status":"hooked"}]`)
	stubDeaconMergeQueue(t, pendingIndex("gt-work1"), nil)

	res, err := ScanStaleHooks(townRoot, DefaultStaleHookConfig())
	if err != nil {
		t.Fatalf("ScanStaleHooks: %v", err)
	}
	if res.StaleCount != 1 || len(res.Results) != 1 {
		t.Fatalf("result = %+v, want one stale hook", res)
	}
	r := res.Results[0]
	if r.Unhooked || r.MergeQueue != string(beads.ReleaseParked) {
		t.Fatalf("result = %+v, want parked, not unhooked", r)
	}
	calls := deaconCalls(t, logPath)
	if strings.Contains(calls, "--status=open") {
		t.Fatalf("held bead must not be reopened, calls:\n%s", calls)
	}
	if !strings.Contains(calls, "update gt-work1 --status=in_progress --assignee=") {
		t.Fatalf("expected the bead to be parked, calls:\n%s", calls)
	}
}

func TestScanStaleHooks_UnhooksBeadNotHeld(t *testing.T) {
	townRoot, logPath := installDeaconBDStub(t, staleHookedJSON, `[{"id":"gt-work1","status":"hooked"}]`)
	stubDeaconMergeQueue(t, beads.MergeQueueIndex{}, nil)

	res, err := ScanStaleHooks(townRoot, DefaultStaleHookConfig())
	if err != nil {
		t.Fatalf("ScanStaleHooks: %v", err)
	}
	if res.Unhooked != 1 || !res.Results[0].Unhooked {
		t.Fatalf("result = %+v, want unhooked", res.Results[0])
	}
	if !strings.Contains(deaconCalls(t, logPath), "update gt-work1 --status=open") {
		t.Fatalf("expected the bead to be reopened")
	}
}

func TestScanStaleHooks_FailsClosedWhenMergeQueueUnreadable(t *testing.T) {
	townRoot, logPath := installDeaconBDStub(t, staleHookedJSON, `[{"id":"gt-work1","status":"hooked"}]`)
	stubDeaconMergeQueue(t, nil, errors.New("dolt: connection refused"))

	res, err := ScanStaleHooks(townRoot, DefaultStaleHookConfig())
	if err != nil {
		t.Fatalf("ScanStaleHooks: %v", err)
	}
	if res.Unhooked != 0 || res.Results[0].Unhooked || res.Results[0].Error == "" {
		t.Fatalf("result = %+v, want an error and no unhook", res.Results[0])
	}
	if strings.Contains(deaconCalls(t, logPath), "update") {
		t.Fatal("no write expected when the merge queue is unreadable")
	}
}

// Redispatch never re-slings an open bead whose MR is pending or landed.
func TestRedispatch_SkipsBeadHeldByMergeQueue(t *testing.T) {
	townRoot, _ := installDeaconBDStub(t, `[]`, `[{"id":"gt-work1","status":"open"}]`)
	var slung []string
	old := slingBeadFn
	slingBeadFn = func(townRoot, beadID, rig, agent string) error {
		slung = append(slung, beadID)
		return nil
	}
	t.Cleanup(func() { slingBeadFn = old })

	stubDeaconMergeQueue(t, pendingIndex("gt-work1"), nil)
	res := Redispatch(townRoot, "gt-work1", "gastown", 0, 0)
	if res.Action != "skipped" || !strings.Contains(res.Message, "merge queue") {
		t.Fatalf("result = %+v, want skipped (held by merge queue)", res)
	}

	stubDeaconMergeQueue(t, nil, errors.New("dolt: connection refused"))
	if res := Redispatch(townRoot, "gt-work1", "gastown", 0, 0); res.Action != "skipped" {
		t.Fatalf("result = %+v, want skipped when the merge queue is unreadable", res)
	}
	if len(slung) != 0 {
		t.Fatalf("held bead was re-slung: %v", slung)
	}

	stubDeaconMergeQueue(t, beads.MergeQueueIndex{}, nil)
	if res := Redispatch(townRoot, "gt-work1", "gastown", 0, 0); res.Action != "redispatched" || len(slung) != 1 {
		t.Fatalf("result = %+v (slung %v), want a re-dispatch when no MR holds the bead", res, slung)
	}
}
