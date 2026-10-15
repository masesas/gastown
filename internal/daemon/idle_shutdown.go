package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/constants"
	"github.com/steveyegge/gastown/internal/events"
	"github.com/steveyegge/gastown/internal/session"
	"github.com/steveyegge/gastown/internal/util"
)

const (
	// defaultIdleShutdownTimeout is how long the town must be continuously
	// idle before the daemon runs `gt down`.
	defaultIdleShutdownTimeout = 30 * time.Minute

	// idleShutdownCheckInterval is how often the idle snapshot is taken.
	// Internal, not user-configurable.
	idleShutdownCheckInterval = time.Minute

	// idleShutdownMailTimeout bounds the overseer mail so a hung send cannot
	// delay the shutdown.
	idleShutdownMailTimeout = 30 * time.Second

	// idleShutdownQueryTimeout bounds the in-process bead queries per check.
	idleShutdownQueryTimeout = 10 * time.Second
)

// IdleShutdownConfig configures the opt-in idle_shutdown patrol.
// User opts in via daemon.json:
//
//	"idle_shutdown": {"enabled": true, "idle_timeout": "30m"}
//
// When the town has had no activity for idle_timeout the daemon runs `gt down`
// (default behavior: not --all, not --nuke).
type IdleShutdownConfig struct {
	// Enabled controls whether idle shutdown runs. Default: false (opt-in).
	Enabled bool `json:"enabled"`

	// IdleTimeout is a Go duration (e.g. "30m"). Default: 30m.
	IdleTimeout string `json:"idle_timeout,omitempty"`
}

// idleShutdownTimeout returns the configured idle timeout, or the default
// (30m) when unset, unparsable or non-positive.
func idleShutdownTimeout(config *DaemonPatrolConfig) time.Duration {
	if config == nil || config.Patrols == nil || config.Patrols.IdleShutdown == nil {
		return defaultIdleShutdownTimeout
	}
	d, err := time.ParseDuration(config.Patrols.IdleShutdown.IdleTimeout)
	if err != nil || d <= 0 {
		return defaultIdleShutdownTimeout
	}
	return d
}

// idleSnapshot is a point-in-time view of town activity.
type idleSnapshot struct {
	PolecatSessions int   // live polecat tmux sessions, all rigs
	ActiveBeads     int   // beads with status hooked or in_progress, town + rigs
	PendingMRs      int   // non-terminal merge requests, all rigs
	HumanClients    int   // tmux clients attached on the town socket
	Err             error // any lookup that could not be determined
}

// active reports whether the town is busy. An undeterminable lookup counts as
// active (fail-safe: never shut down on uncertainty).
func (s idleSnapshot) active() bool {
	return s.Err != nil || s.PolecatSessions > 0 || s.ActiveBeads > 0 ||
		s.PendingMRs > 0 || s.HumanClients > 0
}

// idleState is the tracker carried between checks.
type idleState struct {
	idleSince time.Time // zero while the town is active
	fired     bool      // shutdown already triggered for this idle period
}

type idleAction int

const (
	idleWait idleAction = iota
	idleShutdown
)

// evaluateIdle is the pure decision function: previous state + snapshot + now
// yields the next state and whether to shut down. Activity resets the timer and
// the fired latch; shutdown triggers at most once per continuous idle period.
func evaluateIdle(st idleState, snap idleSnapshot, now time.Time, timeout time.Duration) (idleState, idleAction) {
	if snap.active() {
		return idleState{}, idleWait
	}
	if st.idleSince.IsZero() {
		st.idleSince = now
	}
	if st.fired || now.Sub(st.idleSince) < timeout {
		return st, idleWait
	}
	st.fired = true
	return st, idleShutdown
}

// releaseIdleLatch is applied when `gt down` could not be started or failed:
// it clears the fired latch and restarts the idle clock at now, so a retry
// happens only after one more full idle period.
func releaseIdleLatch(now time.Time) idleState {
	return idleState{idleSince: now}
}

// runIdleShutdown takes an activity snapshot and runs `gt down` once the town
// has been idle for the configured timeout.
func (d *Daemon) runIdleShutdown() {
	if !d.isPatrolActive("idle_shutdown") {
		return
	}
	timeout := idleShutdownTimeout(d.patrolConfig)
	snap := d.collectIdleSnapshot()
	if snap.Err != nil {
		d.logger.Printf("idle_shutdown: activity undeterminable, treating as active: %v", snap.Err)
	}

	now := time.Now()
	if d.idleDownFailed.Swap(false) && d.idle.fired {
		d.logger.Printf("idle_shutdown: previous gt down failed, retrying after another %v idle", timeout)
		d.idle = releaseIdleLatch(now)
	}
	next, action := evaluateIdle(d.idle, snap, now, timeout)
	d.idle = next
	if action != idleShutdown {
		return
	}
	if !d.triggerIdleShutdown(timeout) {
		d.idle = releaseIdleLatch(now)
	}
}

// collectIdleSnapshot gathers the four activity signals. Errors are recorded
// in the snapshot (joined), never swallowed. Bead/MR reads go through the
// daemon's in-process stores (no subprocess per tick).
func (d *Daemon) collectIdleSnapshot() idleSnapshot {
	var snap idleSnapshot
	var errs []error

	rigs := d.getKnownRigs()
	for _, rigName := range rigs {
		n, err := d.countRigPolecatSessions(rigName)
		if err != nil {
			errs = append(errs, fmt.Errorf("polecat sessions (%s): %w", rigName, err))
		}
		snap.PolecatSessions += n
	}

	// Every store (hq + each known rig) must be readable; a missing store
	// means we cannot rule out activity.
	for _, name := range append([]string{"hq"}, rigs...) {
		if _, ok := d.beadsStores[name]; !ok {
			errs = append(errs, fmt.Errorf("beads store %q unavailable", name))
		}
	}
	ctx, cancel := context.WithTimeout(d.ctx, idleShutdownQueryTimeout)
	defer cancel()
	work, mrs, err := countStoreActivity(ctx, d.beadsStores)
	if err != nil {
		errs = append(errs, err)
	}
	snap.ActiveBeads, snap.PendingMRs = work, mrs

	clients, err := d.tmux.ListClients()
	if err != nil {
		errs = append(errs, fmt.Errorf("tmux clients: %w", err))
	}
	snap.HumanClients = len(clients)

	snap.Err = errors.Join(errs...)
	return snap
}

// countRigPolecatSessions counts live tmux sessions for the rig's polecats.
func (d *Daemon) countRigPolecatSessions(rigName string) (int, error) {
	polecats, err := listPolecatWorktrees(filepath.Join(d.config.TownRoot, rigName, "polecats"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // rig has no polecats directory
		}
		return 0, err
	}
	count := 0
	for _, name := range polecats {
		alive, err := d.tmux.HasSession(session.PolecatSessionName(session.PrefixFor(rigName), name))
		if err != nil {
			return count, err
		}
		if alive {
			count++
		}
	}
	return count, nil
}

// isActiveWorkBead reports whether a hooked/in_progress bead represents real
// work in flight. Patrol wisps (deacon/witness/refinery) are intentionally kept
// hooked by their agents, and agent beads are identity records; neither is work.
// Work for dogs, polecats, crew and mayor has an ordinary title and counts.
func isActiveWorkBead(issue *beads.Issue) bool {
	if issue == nil || beads.IsAgentBead(issue) {
		return false
	}
	for _, mol := range []string{constants.MolDeaconPatrol, constants.MolWitnessPatrol, constants.MolRefineryPatrol} {
		if strings.HasPrefix(issue.Title, mol) {
			return false
		}
	}
	return true
}

// sdkIssueToBead projects the fields isActiveWorkBead needs.
func sdkIssueToBead(i *beadsdk.Issue) *beads.Issue {
	return &beads.Issue{Title: i.Title, Type: string(i.IssueType), Status: string(i.Status), Labels: i.Labels}
}

// countStoreActivity counts real active work (hooked/in_progress, patrol wisps
// and agent beads excluded) and non-terminal merge requests across all stores.
// SearchIssues spans both the issues and wisps tables. The first error is
// returned together with the partial counts.
func countStoreActivity(ctx context.Context, stores map[string]beadsdk.Storage) (work, mrs int, err error) {
	var errs []error
	for name, store := range stores {
		active, e := store.SearchIssues(ctx, "", beadsdk.IssueFilter{
			Statuses: []beadsdk.Status{beadsdk.Status("hooked"), beadsdk.Status("in_progress")},
		})
		if e != nil {
			errs = append(errs, fmt.Errorf("active beads (%s): %w", name, e))
		}
		for _, i := range active {
			if isActiveWorkBead(sdkIssueToBead(i)) {
				work++
			}
		}

		queue, e := store.SearchIssues(ctx, "", beadsdk.IssueFilter{Labels: []string{"gt:merge-request"}})
		if e != nil {
			errs = append(errs, fmt.Errorf("merge queue (%s): %w", name, e))
		}
		for _, i := range queue {
			if !beads.IssueStatus(i.Status).IsTerminal() {
				mrs++
			}
		}
	}
	return work, mrs, errors.Join(errs...)
}

// triggerIdleShutdown returns false when gt down could not be started. It logs, notifies the overseer, emits a feed event and then
// launches `gt down` detached. gt down stops this daemon, so the process is
// started in its own process group with no daemon-bound context, and is not
// waited on synchronously.
func (d *Daemon) triggerIdleShutdown(timeout time.Duration) bool {
	d.logger.Printf("idle_shutdown: town idle for %v, running gt down", timeout)

	d.notifyOverseerOfIdleShutdown(timeout)
	_ = events.LogFeed(events.TypeIdleShutdown, "daemon", map[string]interface{}{
		"idle_timeout": timeout.String(),
	})

	cmd := exec.Command(d.gtPath, "down") //nolint:gosec // G204: args are constants
	util.SetDetachedProcessGroup(cmd)
	cmd.Dir = d.config.TownRoot
	cmd.Env = append(os.Environ(), "BD_ACTOR=daemon")
	if err := cmd.Start(); err != nil {
		d.logger.Printf("idle_shutdown: failed to start gt down: %v", err)
		return false
	}
	d.logger.Printf("idle_shutdown: gt down started (pid %d)", cmd.Process.Pid)
	go func() {
		if err := cmd.Wait(); err != nil {
			d.logger.Printf("idle_shutdown: gt down exited with error: %v", err)
			d.idleDownFailed.Store(true)
		}
	}()
	return true
}

// notifyOverseerOfIdleShutdown mails the human overseer, bounded by a timeout.
// Best effort: a failed mail never blocks the shutdown.
func (d *Daemon) notifyOverseerOfIdleShutdown(timeout time.Duration) {
	subject := "IDLE_SHUTDOWN: town idle, shutting down"
	body := fmt.Sprintf("No polecat sessions, hooked/in_progress beads, pending merge requests or "+
		"attached tmux clients for %v. The daemon is running `gt down` (idle_shutdown patrol). "+
		"Restart with `gt up`.", timeout)
	ctx, cancel := context.WithTimeout(context.Background(), idleShutdownMailTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, d.gtPath, "mail", "send", "--human", "-s", subject, "-m", body) //nolint:gosec // G204: args are constructed internally
	setSysProcAttr(cmd)
	cmd.Dir = d.config.TownRoot
	cmd.Env = append(os.Environ(), "BD_ACTOR=daemon")
	if err := cmd.Run(); err != nil {
		d.logger.Printf("idle_shutdown: failed to mail overseer: %v", err)
	}
}
