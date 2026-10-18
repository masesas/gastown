package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergescope"
	"github.com/steveyegge/gastown/internal/refinery"
)

// writeMQScopeFile writes a file (creating parent dirs) inside repo.
func writeMQScopeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeMQScopeRigSettings writes <rigPath>/settings/config.json with the
// given merge_queue section (nil writes settings without merge_queue).
func writeMQScopeRigSettings(t *testing.T, rigPath string, mq map[string]any) {
	t.Helper()
	settings := map[string]any{"type": "rig-settings", "version": 1}
	if mq != nil {
		settings["merge_queue"] = mq
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(rigPath, "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// initMQScopeRepo builds origin (bare) + clone with main pushed and a
// feature branch "polecat/nux/gt-1" that changes the given paths and is
// pushed too. Returns the clone path.
func initMQScopeRepo(t *testing.T, featurePaths ...string) string {
	t.Helper()
	remote := t.TempDir()
	runGitForMQSubmitTest(t, remote, "init", "--bare")
	repo := t.TempDir()
	runGitForMQSubmitTest(t, repo, "init")
	runGitForMQSubmitTest(t, repo, "config", "user.email", "test@example.com")
	runGitForMQSubmitTest(t, repo, "config", "user.name", "Test User")
	runGitForMQSubmitTest(t, repo, "remote", "add", "origin", remote)
	writeMQScopeFile(t, repo, "apps/mobile/main.dart", "v1\n")
	writeMQScopeFile(t, repo, "docs/guide.md", "guide\n")
	runGitForMQSubmitTest(t, repo, "add", "-A")
	runGitForMQSubmitTest(t, repo, "commit", "-m", "main")
	runGitForMQSubmitTest(t, repo, "branch", "-M", "main")
	runGitForMQSubmitTest(t, repo, "push", "-u", "origin", "main")

	runGitForMQSubmitTest(t, repo, "checkout", "-b", "polecat/nux/gt-1")
	for _, p := range featurePaths {
		writeMQScopeFile(t, repo, p, "changed\n")
	}
	runGitForMQSubmitTest(t, repo, "add", "-A")
	runGitForMQSubmitTest(t, repo, "commit", "-m", "feature")
	runGitForMQSubmitTest(t, repo, "push", "-u", "origin", "polecat/nux/gt-1")
	return repo
}

type fakeMQScopeManager struct {
	mr  *refinery.MergeRequest
	err error
}

func (f fakeMQScopeManager) FindMRForPostMerge(string) (*refinery.MergeRequest, error) {
	return f.mr, f.err
}

func TestRunMQCheckScopeFor(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}
	mr := &refinery.MergeRequest{ID: "gt-mr-1", Branch: "polecat/nux/gt-1", TargetBranch: "main"}

	t.Run("in-scope MR passes", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		out, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(repo), "gt-mr-1")
		if err != nil {
			t.Fatalf("runMQCheckScopeFor() = %v", err)
		}
		if !strings.Contains(out, "within allowed_paths") {
			t.Fatalf("output %q missing pass summary", out)
		}
		tip := runGitForMQSubmitTest(t, repo, "rev-parse", "origin/polecat/nux/gt-1")
		if !strings.Contains(out, "\nChecked-SHA: "+tip) {
			t.Fatalf("output %q missing machine-readable Checked-SHA: %s line", out, tip)
		}
	})

	t.Run("Checked-SHA is the freshly fetched tip that was diffed", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart")
		stale := runGitForMQSubmitTest(t, repo, "rev-parse", "origin/polecat/nux/gt-1")
		writeMQScopeFile(t, repo, "apps/mobile/more.dart", "more\n")
		runGitForMQSubmitTest(t, repo, "add", "-A")
		runGitForMQSubmitTest(t, repo, "commit", "-m", "more")
		runGitForMQSubmitTest(t, repo, "push", "origin", "polecat/nux/gt-1")
		fresh := runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		runGitForMQSubmitTest(t, repo, "update-ref", "refs/remotes/origin/polecat/nux/gt-1", stale)

		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		out, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(repo), "gt-mr-1")
		if err != nil {
			t.Fatalf("runMQCheckScopeFor() = %v", err)
		}
		if !strings.Contains(out, "Checked-SHA: "+fresh) || strings.Contains(out, stale) {
			t.Fatalf("output %q: want Checked-SHA %s (fresh), not stale %s", out, fresh, stale)
		}
	})

	t.Run("branch re-pushed out of scope after submit is rejected", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart")
		// Re-push after submit: add an out-of-scope file on the remote branch,
		// then rewind the local tracking ref so only a fetch reveals it.
		before := runGitForMQSubmitTest(t, repo, "rev-parse", "origin/polecat/nux/gt-1")
		writeMQScopeFile(t, repo, ".brief-reviews/x.md", "review\n")
		runGitForMQSubmitTest(t, repo, "add", "-A")
		runGitForMQSubmitTest(t, repo, "commit", "-m", "review notes")
		runGitForMQSubmitTest(t, repo, "push", "origin", "polecat/nux/gt-1")
		runGitForMQSubmitTest(t, repo, "update-ref", "refs/remotes/origin/polecat/nux/gt-1", before)

		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		_, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(repo), "gt-mr-1")
		var scopeErr *mergescope.Error
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{".brief-reviews/x.md"}) {
			t.Fatalf("runMQCheckScopeFor() = %v, want violation [.brief-reviews/x.md]", err)
		}
	})

	t.Run("unrestricted rig passes without git", func(t *testing.T) {
		rig := t.TempDir()
		out, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-1")
		if err != nil {
			t.Fatalf("runMQCheckScopeFor() = %v", err)
		}
		if !strings.Contains(out, "unrestricted") {
			t.Fatalf("output %q missing unrestricted notice", out)
		}
	})

	t.Run("lookup failure fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		_, err := runMQCheckScopeFor(fakeMQScopeManager{err: refinery.ErrMRNotFound}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-x")
		if err == nil {
			t.Fatal("runMQCheckScopeFor() with missing MR = nil, want error")
		}
	})

	t.Run("MR without branch or target fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		for _, bad := range []*refinery.MergeRequest{
			{ID: "gt-mr-2", TargetBranch: "main"},
			{ID: "gt-mr-3", Branch: "polecat/nux/gt-1"},
		} {
			if _, err := runMQCheckScopeFor(fakeMQScopeManager{mr: bad}, rig, gitpkg.NewGit(t.TempDir()), bad.ID); err == nil {
				t.Fatalf("runMQCheckScopeFor(%+v) = nil, want error", bad)
			}
		}
	})

	t.Run("fetch failure fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		if _, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-1"); err == nil {
			t.Fatal("runMQCheckScopeFor() without a repo = nil, want error")
		}
	})
}

func TestEnforceSubmitMergeScope(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}

	t.Run("gt mq submit gate checks the submitted commit", func(t *testing.T) {
		repo := initMQScopeRepo(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		head := runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		runGitForMQSubmitTest(t, repo, "checkout", "main") // --branch submit: HEAD differs
		g := gitpkg.NewGit(repo)
		err := enforceSubmitMergeScope(rig, g, "main", "main", "polecat/nux/gt-1", head)
		var scopeErr *mergescope.Error
		if !errors.As(err, &scopeErr) {
			t.Fatalf("enforceSubmitMergeScope(sha) = %v, want *mergescope.Error", err)
		}
		err = enforceSubmitMergeScope(rig, g, "main", "main", "polecat/nux/gt-1", "")
		if !errors.As(err, &scopeErr) {
			t.Fatalf("enforceSubmitMergeScope(branch fallback) = %v, want *mergescope.Error", err)
		}
	})

	t.Run("gt mq submit gate is a no-op without allowed_paths", func(t *testing.T) {
		rig := t.TempDir()
		g := gitpkg.NewGit(t.TempDir())
		if err := enforceSubmitMergeScope(rig, g, "main", "main", "b", ""); err != nil {
			t.Fatalf("enforceSubmitMergeScope() = %v, want nil", err)
		}
	})
}

func TestRunMQCheckScopeRange(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}
	setup := func(t *testing.T, paths ...string) (repo, rig, old, head string) {
		t.Helper()
		repo = initMQScopeRepo(t, "apps/mobile/main.dart")
		old = runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		for _, p := range paths {
			writeMQScopeFile(t, repo, p, "pushed\n")
		}
		runGitForMQSubmitTest(t, repo, "add", "-A")
		runGitForMQSubmitTest(t, repo, "commit", "-m", "pushed")
		head = runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		rig = t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		return repo, rig, old, head
	}

	t.Run("in-scope push passes", func(t *testing.T) {
		repo, rig, old, head := setup(t, "contracts/openapi.yaml")
		out, err := runMQCheckScopeRange(rig, gitpkg.NewGit(repo), old, head)
		if err != nil {
			t.Fatalf("runMQCheckScopeRange() = %v", err)
		}
		if !strings.Contains(out, "1 changed path(s) within allowed_paths") || !strings.Contains(out, old+".."+head) {
			t.Fatalf("output %q missing range summary", out)
		}
	})

	t.Run("out-of-scope push is rejected with two-dot range", func(t *testing.T) {
		repo, rig, old, head := setup(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md")
		_, err := runMQCheckScopeRange(rig, gitpkg.NewGit(repo), old, head)
		var scopeErr *mergescope.Error
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{".brief-reviews/kp-4az-r1/mobile-consumer.md"}) {
			t.Fatalf("runMQCheckScopeRange() = %v, want violation", err)
		}
		if !strings.Contains(err.Error(), old+".."+head) || strings.Contains(err.Error(), old+"..."+head) {
			t.Fatalf("error %q should name the two-dot range", err)
		}
	})

	t.Run("unrestricted rig passes without git", func(t *testing.T) {
		out, err := runMQCheckScopeRange(t.TempDir(), gitpkg.NewGit(t.TempDir()), "a", "b")
		if err != nil || !strings.Contains(out, "unrestricted") {
			t.Fatalf("runMQCheckScopeRange() = %q, %v; want unrestricted pass", out, err)
		}
	})

	t.Run("fail closed", func(t *testing.T) {
		repo, rig, _, head := setup(t, "apps/x.dart")
		g := gitpkg.NewGit(repo)
		for name, rng := range map[string][2]string{
			"zero old sha (new branch)": {"0000000000000000000000000000000000000000", head},
			"unknown ref":               {"no-such-ref", head},
			"empty old":                 {"", head},
			"option-looking ref":        {"--output=/tmp/x", head},
			"option-looking new":        {head, "-x"},
		} {
			if _, err := runMQCheckScopeRange(rig, g, rng[0], rng[1]); err == nil {
				t.Errorf("%s: runMQCheckScopeRange(%q, %q) = nil, want error", name, rng[0], rng[1])
			}
		}
		bad := t.TempDir()
		writeMQScopeRigSettings(t, bad, map[string]any{"allowed_paths": []string{"/abs/**"}})
		if _, err := runMQCheckScopeRange(bad, g, head, head); err == nil {
			t.Error("invalid pattern: want fail-closed error")
		}
	})
}
