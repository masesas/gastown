package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/constants"
)

func TestIdleShutdownTimeout_Default(t *testing.T) {
	if got := idleShutdownTimeout(nil); got != 30*time.Minute {
		t.Errorf("nil config: got %v, want 30m", got)
	}
	cfg := &DaemonPatrolConfig{Patrols: &PatrolsConfig{IdleShutdown: &IdleShutdownConfig{Enabled: true}}}
	if got := idleShutdownTimeout(cfg); got != defaultIdleShutdownTimeout {
		t.Errorf("empty idle_timeout: got %v, want %v", got, defaultIdleShutdownTimeout)
	}
	if defaultIdleShutdownTimeout != 30*time.Minute {
		t.Errorf("defaultIdleShutdownTimeout = %v, want 30m", defaultIdleShutdownTimeout)
	}
}

func TestIdleShutdownTimeout_Configured(t *testing.T) {
	mk := func(s string) *DaemonPatrolConfig {
		return &DaemonPatrolConfig{Patrols: &PatrolsConfig{IdleShutdown: &IdleShutdownConfig{Enabled: true, IdleTimeout: s}}}
	}
	if got := idleShutdownTimeout(mk("10m")); got != 10*time.Minute {
		t.Errorf("10m: got %v", got)
	}
	for _, bad := range []string{"garbage", "0s", "-5m"} {
		if got := idleShutdownTimeout(mk(bad)); got != defaultIdleShutdownTimeout {
			t.Errorf("%q: got %v, want default", bad, got)
		}
	}
}

func TestIdleShutdown_OptIn(t *testing.T) {
	if IsPatrolEnabled(nil, "idle_shutdown") {
		t.Error("nil config must not enable idle_shutdown")
	}
	if IsPatrolEnabled(&DaemonPatrolConfig{Patrols: &PatrolsConfig{}}, "idle_shutdown") {
		t.Error("unset idle_shutdown must be disabled")
	}
	if IsPatrolEnabled(&DaemonPatrolConfig{Patrols: &PatrolsConfig{IdleShutdown: &IdleShutdownConfig{}}}, "idle_shutdown") {
		t.Error("enabled=false must be disabled")
	}
	if !IsPatrolEnabled(&DaemonPatrolConfig{Patrols: &PatrolsConfig{IdleShutdown: &IdleShutdownConfig{Enabled: true}}}, "idle_shutdown") {
		t.Error("enabled=true must be enabled")
	}
}

func TestIdleShutdown_NotInLifecycleDefaults(t *testing.T) {
	cfg := &DaemonPatrolConfig{Patrols: &PatrolsConfig{}}
	EnsureLifecycleDefaults(cfg)
	if cfg.Patrols.IdleShutdown != nil {
		t.Error("idle_shutdown is opt-in and must not be added by EnsureLifecycleDefaults")
	}
	if DefaultLifecycleConfig().Patrols.IdleShutdown != nil {
		t.Error("idle_shutdown must not be in DefaultLifecycleConfig")
	}
}

func TestIdleSnapshot_Active(t *testing.T) {
	cases := []struct {
		name string
		snap idleSnapshot
		want bool
	}{
		{"all idle", idleSnapshot{}, false},
		{"polecat session", idleSnapshot{PolecatSessions: 1}, true},
		{"active bead", idleSnapshot{ActiveBeads: 2}, true},
		{"pending MR", idleSnapshot{PendingMRs: 1}, true},
		{"human client", idleSnapshot{HumanClients: 1}, true},
		{"lookup error", idleSnapshot{Err: errors.New("dolt down")}, true},
	}
	for _, c := range cases {
		if got := c.snap.active(); got != c.want {
			t.Errorf("%s: active()=%v, want %v", c.name, got, c.want)
		}
	}
}

func TestEvaluateIdle_Lifecycle(t *testing.T) {
	const timeout = 30 * time.Minute
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	idle := idleSnapshot{}

	// First idle observation starts the clock; no shutdown yet.
	st, act := evaluateIdle(idleState{}, idle, t0, timeout)
	if act != idleWait || !st.idleSince.Equal(t0) {
		t.Fatalf("first idle: act=%v since=%v", act, st.idleSince)
	}

	// Just under threshold: wait, clock unchanged.
	st, act = evaluateIdle(st, idle, t0.Add(timeout-time.Second), timeout)
	if act != idleWait || !st.idleSince.Equal(t0) {
		t.Fatalf("under threshold: act=%v since=%v", act, st.idleSince)
	}

	// At threshold: shutdown.
	st, act = evaluateIdle(st, idle, t0.Add(timeout), timeout)
	if act != idleShutdown || !st.fired {
		t.Fatalf("at threshold: act=%v fired=%v", act, st.fired)
	}

	// Once per idle period: no second shutdown while still idle.
	st, act = evaluateIdle(st, idle, t0.Add(timeout+time.Hour), timeout)
	if act != idleWait || !st.fired {
		t.Fatalf("after fire: act=%v fired=%v", act, st.fired)
	}

	// Activity resets everything, including the fired latch.
	st, act = evaluateIdle(st, idleSnapshot{HumanClients: 1}, t0.Add(2*time.Hour), timeout)
	if act != idleWait || !st.idleSince.IsZero() || st.fired {
		t.Fatalf("activity reset: act=%v state=%+v", act, st)
	}

	// A new idle period can shut down again.
	t1 := t0.Add(3 * time.Hour)
	st, _ = evaluateIdle(st, idle, t1, timeout)
	_, act = evaluateIdle(st, idle, t1.Add(timeout), timeout)
	if act != idleShutdown {
		t.Fatalf("second period: act=%v", act)
	}
}

func TestEvaluateIdle_ActivityResetsTimer(t *testing.T) {
	const timeout = 30 * time.Minute
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st, _ := evaluateIdle(idleState{}, idleSnapshot{}, t0, timeout)

	// Blip of activity 20m in restarts the clock.
	st, act := evaluateIdle(st, idleSnapshot{PolecatSessions: 1}, t0.Add(20*time.Minute), timeout)
	if act != idleWait || !st.idleSince.IsZero() {
		t.Fatalf("blip: act=%v state=%+v", act, st)
	}
	// 40m after t0 would have exceeded the threshold had the timer not reset.
	st, act = evaluateIdle(st, idleSnapshot{}, t0.Add(40*time.Minute), timeout)
	if act != idleWait || !st.idleSince.Equal(t0.Add(40*time.Minute)) {
		t.Fatalf("after blip: act=%v state=%+v", act, st)
	}
}

func TestEvaluateIdle_EachConditionBlocks(t *testing.T) {
	const timeout = time.Minute
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	blockers := map[string]idleSnapshot{
		"polecat": {PolecatSessions: 1},
		"bead":    {ActiveBeads: 1},
		"mr":      {PendingMRs: 1},
		"client":  {HumanClients: 1},
		"error":   {Err: errors.New("lookup failed")},
	}
	for name, snap := range blockers {
		st, _ := evaluateIdle(idleState{}, idleSnapshot{}, t0, timeout)
		st, act := evaluateIdle(st, snap, t0.Add(timeout*10), timeout)
		if act == idleShutdown {
			t.Errorf("%s: must not shut down", name)
		}
		if !st.idleSince.IsZero() {
			t.Errorf("%s: timer must reset", name)
		}
	}
}

func TestIsActiveWorkBead(t *testing.T) {
	cases := []struct {
		name  string
		issue *beads.Issue
		want  bool
	}{
		{"nil", nil, false},
		{"deacon patrol wisp", &beads.Issue{Title: constants.MolDeaconPatrol + ": cycle"}, false},
		{"witness patrol wisp", &beads.Issue{Title: constants.MolWitnessPatrol}, false},
		{"refinery patrol wisp", &beads.Issue{Title: constants.MolRefineryPatrol + " rig"}, false},
		{"reaper dog formula", &beads.Issue{Title: constants.MolDogReaper}, false},
		{"doctor dog formula", &beads.Issue{Title: constants.MolDogDoctor + ": run"}, false},
		{"backup dog formula", &beads.Issue{Title: constants.MolDogBackup}, false},
		{"jsonl dog formula", &beads.Issue{Title: constants.MolDogJSONL}, false},
		{"compactor dog formula", &beads.Issue{Title: constants.MolDogCompactor}, false},
		{"checkpoint dog formula", &beads.Issue{Title: constants.MolDogCheckpoint}, false},
		{"agent bead by label", &beads.Issue{Title: "gt-gastown-polecat-x", Labels: []string{"gt:agent"}}, false},
		{"agent bead by legacy type", &beads.Issue{Title: "agent", Type: "agent"}, false},
		{"polecat task", &beads.Issue{Title: "Fix the thing", Type: "task"}, true},
		{"dog work", &beads.Issue{Title: "Reap wisps", Assignee: "deacon/dogs/alpha"}, true},
		{"crew/mayor work", &beads.Issue{Title: "Plan release", Assignee: "mayor"}, true},
		{"title merely mentions patrol", &beads.Issue{Title: "Investigate mol-deacon-patrol bug"}, true},
	}
	for _, c := range cases {
		if got := isActiveWorkBead(c.issue, nil); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIsActiveWorkBead_MoleculeRoot(t *testing.T) {
	step := &beads.Issue{Title: "Compact expired wisps", Status: "in_progress"}
	cases := []struct {
		name string
		root *beads.Issue
		want bool
	}{
		{"no root", nil, true},
		{"deacon patrol root", &beads.Issue{Title: constants.MolDeaconPatrol}, false},
		{"witness patrol root", &beads.Issue{Title: constants.MolWitnessPatrol + ": rig"}, false},
		{"dog compactor root", &beads.Issue{Title: constants.MolDogCompactor}, false},
		{"polecat work root", &beads.Issue{Title: "mol-polecat-work"}, true},
		{"human-titled root", &beads.Issue{Title: "Reap wisps"}, true},
	}
	for _, c := range cases {
		if got := isActiveWorkBead(step, c.root); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

type activityStore struct {
	beadsdk.Storage
	active []*beadsdk.Issue
	mrs    []*beadsdk.Issue
	all    []*beadsdk.Issue // lookup table for IDs queries (parents)
	err    error
	// failIDs makes ID lookups fail (parent unreadable).
	failIDs bool
}

func (s *activityStore) SearchIssues(_ context.Context, _ string, f beadsdk.IssueFilter) ([]*beadsdk.Issue, error) {
	if s.err != nil {
		return nil, s.err
	}
	if len(f.IDs) > 0 {
		if s.failIDs {
			return nil, errors.New("lookup failed")
		}
		var out []*beadsdk.Issue
		for _, i := range s.all {
			for _, id := range f.IDs {
				if i.ID == id {
					out = append(out, i)
				}
			}
		}
		return out, nil
	}
	if len(f.Labels) > 0 {
		return s.mrs, nil
	}
	return s.active, nil
}

func TestCountStoreActivity(t *testing.T) {
	stores := map[string]beadsdk.Storage{
		"hq": &activityStore{active: []*beadsdk.Issue{
			{Title: constants.MolDeaconPatrol, Status: "hooked"},
			{Title: "agent", Labels: []string{"gt:agent"}, Status: "hooked"},
		}},
		"rig": &activityStore{
			active: []*beadsdk.Issue{{ID: "gt-task1", Title: "real task", Status: "in_progress"}},
			mrs:    []*beadsdk.Issue{{ID: "gt-mr1", Status: "open"}, {ID: "gt-mr2", Status: "closed"}},
		},
	}
	work, mrs, err := countStoreActivity(context.Background(), stores)
	if err != nil || len(work) != 1 || len(mrs) != 1 {
		t.Fatalf("work=%v mrs=%v err=%v, want 1,1,nil", work, mrs, err)
	}
	if work[0] != "gt-task1" || mrs[0] != "gt-mr1" {
		t.Fatalf("ids work=%v mrs=%v", work, mrs)
	}

	stores["rig"] = &activityStore{err: errors.New("dolt down")}
	if _, _, err := countStoreActivity(context.Background(), stores); err == nil {
		t.Fatal("store error must be reported (fail-safe)")
	}
}

func childOf(id, title, parent string) *beadsdk.Issue {
	return &beadsdk.Issue{ID: id, Title: title, Status: "in_progress", Dependencies: []*beadsdk.Dependency{
		{IssueID: id, DependsOnID: parent, Type: beadsdk.DepParentChild},
	}}
}

func TestCountStoreActivity_MoleculeSteps(t *testing.T) {
	root := func(id, title string) *beadsdk.Issue { return &beadsdk.Issue{ID: id, Title: title, Status: "hooked"} }
	cases := []struct {
		name    string
		active  []*beadsdk.Issue
		all     []*beadsdk.Issue
		failIDs bool
		want    []string
	}{
		{"deacon patrol step ignored",
			[]*beadsdk.Issue{childOf("s1", "Compact expired wisps", "r1")},
			[]*beadsdk.Issue{root("r1", constants.MolDeaconPatrol)}, false, nil},
		{"dog molecule step ignored",
			[]*beadsdk.Issue{childOf("s1", "step", "r1")},
			[]*beadsdk.Issue{root("r1", constants.MolDogReaper+": run")}, false, nil},
		{"polecat work step counts",
			[]*beadsdk.Issue{childOf("s1", "Implement", "r1")},
			[]*beadsdk.Issue{root("r1", "mol-polecat-work")}, false, []string{"s1"}},
		{"nested step reaches ignored root",
			[]*beadsdk.Issue{childOf("s1", "leaf", "m1")},
			[]*beadsdk.Issue{childOf("m1", "mid", "r1"), root("r1", constants.MolRefineryPatrol)}, false, nil},
		{"parent unreadable is active",
			[]*beadsdk.Issue{childOf("s1", "step", "r1")},
			[]*beadsdk.Issue{root("r1", constants.MolDeaconPatrol)}, true, []string{"s1"}},
		{"parent missing is active",
			[]*beadsdk.Issue{childOf("s1", "step", "gone")}, nil, false, []string{"s1"}},
		{"no parent, ordinary task counts",
			[]*beadsdk.Issue{{ID: "t1", Title: "Fix", Status: "in_progress"}}, nil, false, []string{"t1"}},
		{"depth limit stops walk (fail-safe active)",
			[]*beadsdk.Issue{childOf("a", "a", "b")},
			[]*beadsdk.Issue{childOf("b", "b", "c"), childOf("c", "c", "d"), childOf("d", "d", "e"),
				childOf("e", "e", "f"), childOf("f", "f", "g"), root("g", constants.MolDeaconPatrol)}, false, []string{"a"}},
	}
	for _, c := range cases {
		stores := map[string]beadsdk.Storage{"hq": &activityStore{active: c.active, all: c.all, failIDs: c.failIDs}}
		work, _, err := countStoreActivity(context.Background(), stores)
		if err != nil {
			t.Errorf("%s: unexpected err %v", c.name, err)
		}
		if strings.Join(work, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: work=%v, want %v", c.name, work, c.want)
		}
	}
}

func TestReleaseIdleLatch_RetriesAfterOnePeriod(t *testing.T) {
	const timeout = 30 * time.Minute
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st := releaseIdleLatch(t0)
	if st.fired {
		t.Fatal("latch must be released")
	}
	st, act := evaluateIdle(st, idleSnapshot{}, t0.Add(timeout-time.Second), timeout)
	if act != idleWait {
		t.Fatal("must wait a full period before retry")
	}
	if _, act = evaluateIdle(st, idleSnapshot{}, t0.Add(timeout), timeout); act != idleShutdown {
		t.Fatal("must retry after one period")
	}
}

func TestIdleSnapshot_Reason(t *testing.T) {
	snap := idleSnapshot{
		PolecatSessions: 1, ActiveBeads: 7, PendingMRs: 1, HumanClients: 0,
		WorkIDs: []string{"g7", "g1", "g2", "g3", "g4", "g5", "g6"},
		MRIDs:   []string{"mr-1"},
		Err:     errors.New("dolt down\nmore"),
	}
	got := snap.reason()
	for _, want := range []string{"polecats=1", "work=7", "mrs=1", "clients=0",
		"work_ids=g1,g2,g3,g4,g5,+2 more", "mr_ids=mr-1", "error=dolt down"} {
		if !strings.Contains(got, want) {
			t.Errorf("reason %q missing %q", got, want)
		}
	}
	if strings.Contains(snap.reasonKey(), "g1") {
		t.Error("reasonKey must not contain IDs")
	}
}

func TestShouldLogActiveReason(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	const iv = 30 * time.Minute
	if !shouldLogActiveReason("", time.Time{}, "a", t0, iv) {
		t.Error("first log must happen")
	}
	if shouldLogActiveReason("a", t0, "a", t0.Add(time.Minute), iv) {
		t.Error("same reason within interval must not log")
	}
	if !shouldLogActiveReason("a", t0, "b", t0.Add(time.Minute), iv) {
		t.Error("changed reason must log")
	}
	if !shouldLogActiveReason("a", t0, "a", t0.Add(iv), iv) {
		t.Error("same reason after interval must log again")
	}
}
