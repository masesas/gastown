package daemon

import (
	"context"
	"errors"
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
		{"agent bead by label", &beads.Issue{Title: "gt-gastown-polecat-x", Labels: []string{"gt:agent"}}, false},
		{"agent bead by legacy type", &beads.Issue{Title: "agent", Type: "agent"}, false},
		{"polecat task", &beads.Issue{Title: "Fix the thing", Type: "task"}, true},
		{"dog work", &beads.Issue{Title: "Reap wisps", Assignee: "deacon/dogs/alpha"}, true},
		{"crew/mayor work", &beads.Issue{Title: "Plan release", Assignee: "mayor"}, true},
		{"title merely mentions patrol", &beads.Issue{Title: "Investigate mol-deacon-patrol bug"}, true},
	}
	for _, c := range cases {
		if got := isActiveWorkBead(c.issue); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

type activityStore struct {
	beadsdk.Storage
	active []*beadsdk.Issue
	mrs    []*beadsdk.Issue
	err    error
}

func (s *activityStore) SearchIssues(_ context.Context, _ string, f beadsdk.IssueFilter) ([]*beadsdk.Issue, error) {
	if s.err != nil {
		return nil, s.err
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
			active: []*beadsdk.Issue{{Title: "real task", Status: "in_progress"}},
			mrs:    []*beadsdk.Issue{{Status: "open"}, {Status: "closed"}},
		},
	}
	work, mrs, err := countStoreActivity(context.Background(), stores)
	if err != nil || work != 1 || mrs != 1 {
		t.Fatalf("work=%d mrs=%d err=%v, want 1,1,nil", work, mrs, err)
	}

	stores["rig"] = &activityStore{err: errors.New("dolt down")}
	if _, _, err := countStoreActivity(context.Background(), stores); err == nil {
		t.Fatal("store error must be reported (fail-safe)")
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
