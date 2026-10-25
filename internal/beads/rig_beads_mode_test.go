package beads

import (
	"os"
	"path/filepath"
	"testing"
)

// setupRigBeadsLayout creates <town>/<rig> with a tracked-looking
// mayor/rig/.beads (config.yaml) and, when mode != "", a config.json carrying
// beads.mode. Returns (townRoot, rigPath).
func setupRigBeadsLayout(t *testing.T, mode string) (string, string) {
	t.Helper()
	town := t.TempDir()
	rigPath := filepath.Join(town, "myrig")
	mayorBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mayorBeads, "config.yaml"), []byte("issue-prefix: oto\nsync.remote: git+ssh://example.invalid/p.git\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if mode != "" {
		cfg := `{"type":"rig","version":1,"name":"myrig","beads":{"prefix":"mr","mode":"` + mode + `"}}`
		if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(cfg), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return town, rigPath
}

func writeRigOwnDB(t *testing.T, rigPath string) {
	t.Helper()
	rigBeads := filepath.Join(rigPath, ".beads")
	if err := os.MkdirAll(rigBeads, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigBeads, "metadata.json"), []byte(`{"backend":"dolt","dolt_mode":"server","dolt_database":"myrig"}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestTrackedRigBeadsDir(t *testing.T) {
	t.Parallel()

	t.Run("auto with tracked beads", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "")
		dir, ok := TrackedRigBeadsDir(rigPath)
		if !ok || dir != filepath.Join(rigPath, "mayor", "rig", ".beads") {
			t.Fatalf("TrackedRigBeadsDir = (%q, %v), want mayor/rig/.beads, true", dir, ok)
		}
	})

	t.Run("rig mode ignores tracked beads", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "rig")
		if dir, ok := TrackedRigBeadsDir(rigPath); ok {
			t.Fatalf("TrackedRigBeadsDir in rig mode = (%q, true), want false", dir)
		}
	})

	t.Run("no tracked beads", func(t *testing.T) {
		t.Parallel()
		rigPath := filepath.Join(t.TempDir(), "r")
		if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig"), 0755); err != nil {
			t.Fatal(err)
		}
		if _, ok := TrackedRigBeadsDir(rigPath); ok {
			t.Fatal("TrackedRigBeadsDir without mayor/rig/.beads must be false")
		}
	})
}

func TestMayorCloneHasBeads(t *testing.T) {
	t.Parallel()
	_, rigPath := setupRigBeadsLayout(t, "")
	if !MayorCloneHasBeads(rigPath) {
		t.Fatal("MayorCloneHasBeads must be true when mayor/rig/.beads is a directory")
	}
	if MayorCloneHasBeads(filepath.Join(t.TempDir(), "none")) {
		t.Fatal("MayorCloneHasBeads must be false without mayor/rig/.beads")
	}
}

func TestComputeRedirectTarget_RigModeUsesRigOwnBeads(t *testing.T) {
	t.Parallel()
	town, rigPath := setupRigBeadsLayout(t, "rig")
	writeRigOwnDB(t, rigPath)

	tests := []struct {
		worktree string
		want     string
	}{
		{filepath.Join(rigPath, "crew", "alice"), "../../.beads"},
		{filepath.Join(rigPath, "refinery", "rig"), "../../.beads"},
		{filepath.Join(rigPath, "polecats", "p1", "myrig"), "../../../.beads"},
	}
	for _, tt := range tests {
		got, err := ComputeRedirectTarget(town, tt.worktree)
		if err != nil {
			t.Fatalf("ComputeRedirectTarget(%s): %v", tt.worktree, err)
		}
		if got != tt.want {
			t.Errorf("ComputeRedirectTarget(%s) = %q, want %q", tt.worktree, got, tt.want)
		}
	}
}

func TestComputeRedirectTarget_RigModeNeverFallsBackToMayor(t *testing.T) {
	t.Parallel()
	// rig/.beads exists but has no DB marker yet (e.g. bd init failed):
	// in auto mode this falls back to mayor/rig/.beads; in rig mode it must not.
	town, rigPath := setupRigBeadsLayout(t, "rig")
	if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := ComputeRedirectTarget(town, filepath.Join(rigPath, "crew", "alice"))
	if err != nil {
		t.Fatalf("ComputeRedirectTarget: %v", err)
	}
	if got != "../../.beads" {
		t.Fatalf("ComputeRedirectTarget = %q, want ../../.beads (never mayor/rig in rig mode)", got)
	}
}

func TestEnsureMayorCloneRedirect(t *testing.T) {
	t.Parallel()

	t.Run("rig mode writes untracked redirect to rig beads", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "rig")
		writeRigOwnDB(t, rigPath)
		mayorBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
		before, err := os.ReadFile(filepath.Join(mayorBeads, "config.yaml"))
		if err != nil {
			t.Fatal(err)
		}

		wrote, err := EnsureMayorCloneRedirect(rigPath)
		if err != nil || !wrote {
			t.Fatalf("EnsureMayorCloneRedirect = (%v, %v), want (true, nil)", wrote, err)
		}
		data, err := os.ReadFile(filepath.Join(mayorBeads, "redirect"))
		if err != nil {
			t.Fatal(err)
		}
		if string(data) != MayorCloneRedirectTarget+"\n" {
			t.Fatalf("redirect = %q, want %q", data, MayorCloneRedirectTarget+"\n")
		}
		if got := ResolveBeadsDir(filepath.Join(rigPath, "mayor", "rig")); got != filepath.Join(rigPath, ".beads") {
			t.Fatalf("ResolveBeadsDir(mayor/rig) = %q, want rig/.beads", got)
		}
		after, err := os.ReadFile(filepath.Join(mayorBeads, "config.yaml"))
		if err != nil {
			t.Fatalf("tracked config.yaml must survive: %v", err)
		}
		if string(after) != string(before) {
			t.Fatal("tracked config.yaml must not be modified")
		}

		// Idempotent.
		if wrote, err := EnsureMayorCloneRedirect(rigPath); err != nil || wrote {
			t.Fatalf("second EnsureMayorCloneRedirect = (%v, %v), want (false, nil)", wrote, err)
		}
	})

	t.Run("rig mode excludes the redirect in the clone's .git/info/exclude", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "rig")
		gitDir := filepath.Join(rigPath, "mayor", "rig", ".git")
		if err := os.MkdirAll(gitDir, 0755); err != nil {
			t.Fatal(err)
		}
		excludePath := filepath.Join(gitDir, "info", "exclude")
		if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(excludePath, []byte("# git ls-files --others --exclude-from=.git/info/exclude\n*.swp"), 0644); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 2; i++ {
			if _, err := EnsureMayorCloneRedirect(rigPath); err != nil {
				t.Fatalf("EnsureMayorCloneRedirect #%d: %v", i, err)
			}
		}
		data, err := os.ReadFile(excludePath)
		if err != nil {
			t.Fatal(err)
		}
		want := "# git ls-files --others --exclude-from=.git/info/exclude\n*.swp\n" + MayorCloneExcludeEntry + "\n"
		if string(data) != want {
			t.Fatalf("info/exclude = %q, want %q (entry appended once, existing lines kept)", data, want)
		}
	})

	t.Run("rig mode with .git file (worktree) skips exclude gracefully", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "rig")
		if err := os.WriteFile(filepath.Join(rigPath, "mayor", "rig", ".git"), []byte("gitdir: /nowhere\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if wrote, err := EnsureMayorCloneRedirect(rigPath); err != nil || !wrote {
			t.Fatalf("EnsureMayorCloneRedirect = (%v, %v), want (true, nil)", wrote, err)
		}
	})

	t.Run("auto mode is a no-op", func(t *testing.T) {
		t.Parallel()
		_, rigPath := setupRigBeadsLayout(t, "")
		if wrote, err := EnsureMayorCloneRedirect(rigPath); err != nil || wrote {
			t.Fatalf("EnsureMayorCloneRedirect(auto) = (%v, %v), want (false, nil)", wrote, err)
		}
		if _, err := os.Stat(filepath.Join(rigPath, "mayor", "rig", ".beads", "redirect")); !os.IsNotExist(err) {
			t.Fatal("auto mode must not write a mayor clone redirect")
		}
	})

	t.Run("rig mode rejects a non-directory .beads", func(t *testing.T) {
		t.Parallel()
		rigPath := filepath.Join(t.TempDir(), "r")
		if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rigPath, "mayor", "rig", ".beads"), []byte("x"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"beads":{"mode":"rig"}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := EnsureMayorCloneRedirect(rigPath); err == nil {
			t.Fatal("EnsureMayorCloneRedirect must fail when mayor/rig/.beads is not a directory")
		}
	})

	t.Run("rig mode without tracked beads is a no-op", func(t *testing.T) {
		t.Parallel()
		rigPath := filepath.Join(t.TempDir(), "r")
		if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"beads":{"mode":"rig"}}`), 0644); err != nil {
			t.Fatal(err)
		}
		if wrote, err := EnsureMayorCloneRedirect(rigPath); err != nil || wrote {
			t.Fatalf("EnsureMayorCloneRedirect = (%v, %v), want (false, nil)", wrote, err)
		}
		if _, err := os.Stat(filepath.Join(rigPath, "mayor", "rig", ".beads")); !os.IsNotExist(err) {
			t.Fatal("must not create mayor/rig/.beads when the repo does not track it")
		}
	})
}
