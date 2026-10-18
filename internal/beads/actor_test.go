package beads

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
)

func TestWithActor_RejectsInvalidActors(t *testing.T) {
	b := New(t.TempDir())
	for name, actor := range map[string]string{
		"empty":      "",
		"blank":      "  \t ",
		"newline":    "rig/refinery\nBD_ACTOR=x",
		"NUL":        "rig/refinery\x00",
		"DEL":        "rig/\x7frefinery",
		"too long":   strings.Repeat("a", maxActorLen+1),
		"option-ish": "--force",
	} {
		t.Run(name, func(t *testing.T) {
			got, err := b.WithActor(actor)
			if !errors.Is(err, ErrInvalidActor) || got != nil {
				t.Fatalf("WithActor(%q) = (%v, %v), want (nil, ErrInvalidActor)", actor, got, err)
			}
			if strings.ContainsAny(err.Error(), "\n\x00") {
				t.Fatalf("error %q echoes control characters", err)
			}
		})
	}
}

func TestWithActor_ReturnsANewWrapperAndLeavesTheOriginalAlone(t *testing.T) {
	t.Setenv("BD_ACTOR", "inherited/actor")
	b := NewWithBeadsDir(t.TempDir(), t.TempDir())
	scoped, err := b.WithActor("  rig/refinery/merge-1-1 ")
	if err != nil {
		t.Fatal(err)
	}
	if scoped == b {
		t.Fatal("WithActor returned the receiver; it must return a new wrapper")
	}
	if got := scoped.getActor(); got != "rig/refinery/merge-1-1" {
		t.Fatalf("scoped actor = %q, want the trimmed actor", got)
	}
	if got := b.getActor(); got != "inherited/actor" {
		t.Fatalf("original actor = %q, want the inherited BD_ACTOR (receiver mutated)", got)
	}
	if scoped.workDir != b.workDir || scoped.beadsDir != b.beadsDir {
		t.Fatalf("scoped wrapper targets %s/%s, want %s/%s", scoped.workDir, scoped.beadsDir, b.workDir, b.beadsDir)
	}
}

// An explicit actor also applies to an isolated (test) wrapper: isolation
// drops inherited identities, not the one the caller asked for.
func TestWithActor_IsolatedWrapperUsesTheExplicitActor(t *testing.T) {
	t.Setenv("BD_ACTOR", "inherited/actor")
	scoped, err := NewIsolated(t.TempDir()).WithActor("rig/refinery/merge-2-1")
	if err != nil {
		t.Fatal(err)
	}
	if got := scoped.getActor(); got != "rig/refinery/merge-2-1" {
		t.Fatalf("isolated scoped actor = %q", got)
	}
}

func envValues(env []string, key string) []string {
	var out []string
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && strings.EqualFold(k, key) {
			out = append(out, v)
		}
	}
	return out
}

// bd resolves its actor as --actor > BEADS_ACTOR > BD_ACTOR > git user.name >
// $USER. The scoped wrapper must leave exactly one identity for bd to read.
func TestWithActor_SubprocessEnvCarriesExactlyTheActor(t *testing.T) {
	t.Setenv("BD_ACTOR", "inherited/bd")
	t.Setenv("BEADS_ACTOR", "inherited/beads")
	for name, base := range map[string]*Beads{
		"plain":    NewWithBeadsDir(t.TempDir(), t.TempDir()),
		"isolated": NewIsolated(t.TempDir()),
	} {
		t.Run(name, func(t *testing.T) {
			scoped, err := base.WithActor("rig/refinery/merge-3-1")
			if err != nil {
				t.Fatal(err)
			}
			for envName, env := range map[string][]string{"run": scoped.buildRunEnv(), "routing": scoped.buildRoutingEnv()} {
				if got := envValues(env, "BEADS_ACTOR"); len(got) != 1 || got[0] != "rig/refinery/merge-3-1" {
					t.Errorf("%s env BEADS_ACTOR = %v, want exactly [rig/refinery/merge-3-1]", envName, got)
				}
				if got := envValues(env, "BD_ACTOR"); len(got) != 0 {
					t.Errorf("%s env keeps BD_ACTOR %v", envName, got)
				}
			}
		})
	}
}

// Without WithActor the environment is unchanged (no new override).
func TestBuildRunEnv_WithoutActorKeepsInheritedIdentity(t *testing.T) {
	t.Setenv("BD_ACTOR", "inherited/bd")
	env := NewWithBeadsDir(t.TempDir(), t.TempDir()).buildRunEnv()
	if got := envValues(env, "BD_ACTOR"); len(got) != 1 || got[0] != "inherited/bd" {
		t.Fatalf("BD_ACTOR = %v, want the inherited value", got)
	}
}

func TestWithActor_DerivedWrappersKeepTheActor(t *testing.T) {
	town := t.TempDir()
	for _, dir := range []string{"mayor", ".beads", "rig1/.beads", "rig2/.beads"} {
		if err := os.MkdirAll(filepath.Join(town, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(town, "mayor", "town.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteRoutes(filepath.Join(town, ".beads"), []Route{{Prefix: "r1-", Path: "rig1"}, {Prefix: "r2-", Path: "rig2"}}); err != nil {
		t.Fatal(err)
	}
	scoped, err := NewWithBeadsDir(filepath.Join(town, "rig1"), filepath.Join(town, "rig1", ".beads")).WithActor("rig1/refinery/merge-4-1")
	if err != nil {
		t.Fatal(err)
	}
	routed := scoped.forIssueID("r2-abc")
	if routed == scoped {
		t.Fatal("forIssueID did not route r2- to rig2 (test setup)")
	}
	if got := routed.getActor(); got != "rig1/refinery/merge-4-1" {
		t.Errorf("routed wrapper actor = %q, want the scoped actor", got)
	}
	if got := scoped.ForAgentBead().getActor(); got != "rig1/refinery/merge-4-1" {
		t.Errorf("ForAgentBead actor = %q, want the scoped actor", got)
	}
}

// actorRecordingStore records the actor of every in-process write.
type actorRecordingStore struct {
	beadsdk.Storage
	actors []string
}

func (s *actorRecordingStore) UpdateIssue(_ context.Context, _ string, _ map[string]interface{}, actor string) error {
	s.actors = append(s.actors, "update:"+actor)
	return nil
}

func (s *actorRecordingStore) CloseIssue(_ context.Context, _, _, actor, _ string) error {
	s.actors = append(s.actors, "close:"+actor)
	return nil
}

func TestWithActor_StorePathPassesTheActor(t *testing.T) {
	t.Setenv("BD_ACTOR", "inherited/bd")
	store := &actorRecordingStore{}
	scoped, err := NewWithStore(t.TempDir(), store).WithActor("rig/refinery/merge-5-1")
	if err != nil {
		t.Fatal(err)
	}
	empty := ""
	if err := scoped.Update("x-1", UpdateOptions{Assignee: &empty}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.CloseWithReason("merged", "x-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"update:rig/refinery/merge-5-1", "close:rig/refinery/merge-5-1"}
	if strings.Join(store.actors, ",") != strings.Join(want, ",") {
		t.Fatalf("store actors = %v, want %v", store.actors, want)
	}
}

// TestWithActor_RealBDOwnershipGuard runs the installed bd (embedded Dolt, a
// throwaway HOME and repo): bd refuses to close a bead assigned to someone
// else (gt-pd1c.93), a wrapper scoped to the assignee closes it, and --force
// (the source close of a merge) is not subject to the guard.
func TestWithActor_RealBDOwnershipGuard(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the real bd (embedded Dolt init takes seconds)")
	}
	if _, err := exec.LookPath("bd"); err != nil {
		t.Skip("bd not installed")
	}
	repo := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("BD_NON_INTERACTIVE", "1")
	for _, k := range []string{"BD_ACTOR", "BEADS_ACTOR", "BEADS_DIR", "BEADS_DB", "GT_ROOT", "GT_TOWN_ROOT"} {
		t.Setenv(k, "")
		_ = os.Unsetenv(k)
	}
	gitCmd := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = repo
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	gitCmd("init", "-q", ".")
	gitCmd("config", "user.name", "Human Tester")
	gitCmd("config", "user.email", "human@example.test")
	initCmd := exec.Command("bd", "init", "-p", "ag", "--non-interactive", "--quiet")
	initCmd.Dir = repo
	if out, err := initCmd.CombinedOutput(); err != nil {
		t.Skipf("bd init (embedded) unavailable: %v\n%s", err, out)
	}
	human := NewWithBeadsDir(repo, filepath.Join(repo, ".beads"))
	const claimant = "rig/refinery/merge-99-1"

	mr, err := human.Create(CreateOptions{Title: "mr", Priority: 2})
	if err != nil {
		t.Fatal(err)
	}
	assignee := claimant
	if err := human.Update(mr.ID, UpdateOptions{Assignee: &assignee}); err != nil {
		t.Fatal(err)
	}
	if err := human.CloseWithReason("merged", mr.ID); err == nil || !strings.Contains(err.Error(), "cannot close") {
		t.Fatalf("close by the human = %v, want bd's ownership refusal", err)
	}
	scoped, err := human.WithActor(claimant)
	if err != nil {
		t.Fatal(err)
	}
	if err := scoped.CloseWithReason("merged", mr.ID); err != nil {
		t.Fatalf("close as the claimant: %v", err)
	}
	if got, err := human.Show(mr.ID); err != nil || got.Status != "closed" {
		t.Fatalf("MR after close = %+v (%v), want closed", got, err)
	}

	src, err := human.Create(CreateOptions{Title: "src", Priority: 2})
	if err != nil {
		t.Fatal(err)
	}
	polecat := "rig/polecats/nux"
	if err := human.Update(src.ID, UpdateOptions{Assignee: &polecat}); err != nil {
		t.Fatal(err)
	}
	if err := scoped.ForceCloseWithReason("Merged in "+mr.ID, src.ID); err != nil {
		t.Fatalf("forced source close by the claimant: %v", err)
	}
}
