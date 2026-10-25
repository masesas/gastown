package rig

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
)

func TestRigConfig_BeadsModeRoundTrip(t *testing.T) {
	t.Parallel()
	rigPath := t.TempDir()
	m := &Manager{}
	in := &RigConfig{Type: "rig", Version: CurrentRigConfigVersion, Name: "r", Beads: &BeadsConfig{Prefix: "r", Mode: config.RigBeadsModeRig}}
	if err := m.saveRigConfig(rigPath, in); err != nil {
		t.Fatal(err)
	}
	out, err := LoadRigConfig(rigPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Beads == nil || out.Beads.Mode != config.RigBeadsModeRig {
		t.Fatalf("beads.mode lost in round trip: %+v", out.Beads)
	}
	if !config.RigUsesOwnBeads(rigPath) {
		t.Fatal("config.RigUsesOwnBeads must read beads.mode written by saveRigConfig")
	}
}

func TestInitBeads_RigMode_SelfContainedDespiteTrackedBeads(t *testing.T) {
	// Cannot use t.Parallel() due to t.Setenv
	rigPath := filepath.Join(t.TempDir(), "myrig")
	mayorBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
	if err := os.MkdirAll(mayorBeads, 0755); err != nil {
		t.Fatal(err)
	}
	trackedConfig := "issue-prefix: oto\nsync.remote: git+ssh://example.invalid/p.git\n"
	if err := os.WriteFile(filepath.Join(mayorBeads, "config.yaml"), []byte(trackedConfig), 0644); err != nil {
		t.Fatal(err)
	}
	m := &Manager{townRoot: filepath.Dir(rigPath)}
	if err := m.saveRigConfig(rigPath, &RigConfig{Type: "rig", Name: "myrig", Beads: &BeadsConfig{Prefix: "mr", Mode: config.RigBeadsModeRig}}); err != nil {
		t.Fatal(err)
	}

	cmdLog := filepath.Join(t.TempDir(), "bd.log")
	script := "#!/usr/bin/env bash\necho \"$PWD $*\" >> \"$BD_CMD_LOG\"\nexit 0\n"
	windowsScript := "@echo off\r\nexit /b 0\r\n"
	binDir := writeFakeBD(t, script, windowsScript)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_CMD_LOG", cmdLog)

	if err := m.InitBeads(rigPath, "mr", "myrig"); err != nil {
		t.Fatalf("InitBeads: %v", err)
	}

	if _, err := os.Stat(filepath.Join(rigPath, ".beads", "redirect")); !os.IsNotExist(err) {
		t.Fatal("rig mode must not write <rig>/.beads/redirect to mayor/rig/.beads")
	}
	if _, err := os.Stat(filepath.Join(rigPath, ".beads", "config.yaml")); err != nil {
		t.Fatalf("rig mode must create self-contained <rig>/.beads/config.yaml: %v", err)
	}
	if !strings.Contains(readFileOrEmpty(t, filepath.Join(rigPath, ".beads", "metadata.json")), `"dolt_database": "myrig"`) {
		t.Fatalf("rig/.beads/metadata.json must name the rig database; got %q", readFileOrEmpty(t, filepath.Join(rigPath, ".beads", "metadata.json")))
	}
	if got := readFileOrEmpty(t, filepath.Join(mayorBeads, "config.yaml")); got != trackedConfig {
		t.Fatalf("tracked mayor/rig/.beads/config.yaml modified: %q", got)
	}
	if _, err := os.Stat(filepath.Join(mayorBeads, "metadata.json")); !os.IsNotExist(err) {
		t.Fatal("rig mode must not write metadata.json into tracked mayor/rig/.beads")
	}
	// The mayor clone is pointed at the rig's own beads through an untracked redirect.
	if got := beads.ResolveBeadsDir(filepath.Join(rigPath, "mayor", "rig")); got != filepath.Join(rigPath, ".beads") {
		t.Fatalf("ResolveBeadsDir(mayor/rig) = %q, want <rig>/.beads", got)
	}
	if runtime.GOOS != "windows" {
		log := readFileOrEmpty(t, cmdLog)
		if !strings.Contains(log, "init --prefix mr --database myrig --server") {
			t.Fatalf("expected self-contained bd init --server; log:\n%s", log)
		}
		if strings.Contains(log, filepath.Join("mayor", "rig")) {
			t.Fatalf("bd must not run inside the mayor clone in rig mode; log:\n%s", log)
		}
	}
}

func readFileOrEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func TestAddRig_RigMode_LeavesTrackedBeadsUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based bd shim not reliable on Windows CI")
	}

	cmdLog := filepath.Join(t.TempDir(), "bd-cmds.log")
	script := `#!/usr/bin/env bash
cmd="$1"
[[ "$cmd" == "--allow-stale" ]] && { shift; cmd="$1"; }
shift
echo "$cmd $PWD $*" >> "$BD_CMD_LOG"
case "$cmd" in
  init|config|migrate) exit 0 ;;
  show) echo "[]" ;;
  create)
    id=""; title=""
    for arg in "$@"; do
      case "$arg" in --id=*) id="${arg#--id=}" ;; --title=*) title="${arg#--title=}" ;; esac
    done
    printf '{"id":"%s","title":"%s","description":"","issue_type":"agent"}' "$id" "$title"
    ;;
  *) exit 0 ;;
esac
`
	binDir := writeFakeBD(t, script, "")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("BD_CMD_LOG", cmdLog)

	// A project repo that tracks its own beads with a sync.remote (the
	// oto-rent-pro shape), including bd's default .gitignore entry for redirect.
	repoDir := t.TempDir()
	beadsDir := filepath.Join(repoDir, ".beads")
	if err := os.MkdirAll(beadsDir, 0755); err != nil {
		t.Fatal(err)
	}
	trackedConfig := "issue-prefix: oto\nsync.remote: \"git+ssh://git@example.invalid/project.git\"\n"
	trackedMeta := `{"database":"dolt","backend":"dolt","dolt_database":"oto"}` + "\n"
	files := map[string]string{
		"config.yaml":   trackedConfig,
		"metadata.json": trackedMeta,
		".gitignore":    "redirect\n",
		"issues.jsonl":  "{\"id\":\"oto-1\"}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(beadsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"git", "init", "--initial-branch=main", repoDir},
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test User"},
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "project with its own beads"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))
	if _, err := manager.AddRig(AddRigOptions{
		Name:          "otorig",
		GitURL:        repoDir,
		BeadsPrefix:   "or",
		BeadsMode:     config.RigBeadsModeRig,
		SkipDoltCheck: true,
	}); err != nil {
		t.Fatalf("AddRig: %v", err)
	}

	rigPath := filepath.Join(root, "otorig")
	mayorRig := filepath.Join(rigPath, "mayor", "rig")

	// 1. config.json persists the mode and the rig's own prefix (not the project's).
	cfg, err := LoadRigConfig(rigPath)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Beads == nil || cfg.Beads.Mode != config.RigBeadsModeRig || cfg.Beads.Prefix != "or" {
		t.Fatalf("config.json beads = %+v, want prefix or, mode rig", cfg.Beads)
	}
	if entry := manager.config.Rigs["otorig"]; entry.BeadsConfig == nil || entry.BeadsConfig.Mode != config.RigBeadsModeRig {
		t.Fatalf("rigs.json entry must carry beads.mode=rig, got %+v", entry.BeadsConfig)
	}

	// 2. The mayor clone is git-clean and its tracked beads files are byte-identical.
	out, err := exec.Command("git", "-C", mayorRig, "status", "--porcelain", "--ignored=no").CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("mayor clone must stay clean in rig mode, got:\n%s", out)
	}
	sw, err := exec.Command("git", "-C", mayorRig, "ls-files", "-v", ".beads").CombinedOutput()
	if err != nil {
		t.Fatalf("git ls-files -v: %v\n%s", err, sw)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(sw)), "\n") {
		if strings.HasPrefix(line, "S ") {
			t.Fatalf("rig mode must not mark tracked beads files skip-worktree in the mayor clone: %q", line)
		}
	}
	for name, content := range files {
		if got := readFileOrEmpty(t, filepath.Join(mayorRig, ".beads", name)); got != content {
			t.Fatalf("tracked .beads/%s changed in mayor clone: %q", name, got)
		}
	}

	// 3. Rig beads are self-contained; mayor clone + refinery resolve to them.
	if _, err := os.Stat(filepath.Join(rigPath, ".beads", "redirect")); !os.IsNotExist(err) {
		t.Fatal("rig mode must not redirect <rig>/.beads to mayor/rig/.beads")
	}
	rigBeads := filepath.Join(rigPath, ".beads")
	if got := beads.ResolveBeadsDir(mayorRig); got != rigBeads {
		t.Fatalf("ResolveBeadsDir(mayor/rig) = %q, want %q", got, rigBeads)
	}
	if got := beads.ResolveBeadsDir(filepath.Join(rigPath, "refinery", "rig")); got != rigBeads {
		t.Fatalf("ResolveBeadsDir(refinery/rig) = %q, want %q", got, rigBeads)
	}
	if strings.Contains(readFileOrEmpty(t, filepath.Join(rigBeads, "config.yaml")), "sync.remote") {
		t.Fatal("rig beads config must not inherit the project's sync.remote")
	}

	// 4. No bd command ran inside the mayor clone, and no destroy-token was used.
	log := readFileOrEmpty(t, cmdLog)
	if strings.Contains(log, "--destroy-token") || strings.Contains(log, "--discard-remote") {
		t.Fatalf("rig mode must never pass destroy/discard flags; log:\n%s", log)
	}
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "init ") && strings.Contains(line, mayorRig) {
			t.Fatalf("bd init ran inside the mayor clone: %q", line)
		}
	}

	// 5. Route points at the rig root, not mayor/rig.
	routes := readFileOrEmpty(t, filepath.Join(root, ".beads", "routes.jsonl"))
	if !strings.Contains(routes, `"path":"otorig"`) || strings.Contains(routes, "otorig/mayor/rig") {
		t.Fatalf("route for rig mode must be the rig root; routes.jsonl:\n%s", routes)
	}
}

func TestBeadsModeNotice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		mode        string
		tracked     bool
		syncRemote  bool
		wantSubstr  string
		wantWarning bool
	}{
		{name: "rig", mode: config.RigBeadsModeRig, tracked: true, syncRemote: true, wantSubstr: "left untouched"},
		{name: "rig untracked", mode: config.RigBeadsModeRig, wantSubstr: "Beads mode: rig"},
		{name: "auto untracked", mode: config.RigBeadsModeAuto, wantSubstr: "Beads mode: auto"},
		{name: "auto tracked", mode: config.RigBeadsModeAuto, tracked: true, wantSubstr: "mayor/rig/.beads"},
		{name: "auto tracked sync.remote", mode: config.RigBeadsModeAuto, tracked: true, syncRemote: true, wantSubstr: "mayor/rig/.beads", wantWarning: true},
	}
	for _, tt := range tests {
		lines := beadsModeNotice(tt.mode, tt.tracked, tt.syncRemote)
		joined := strings.Join(lines, "\n")
		if !strings.Contains(joined, tt.wantSubstr) {
			t.Errorf("%s: notice %q missing %q", tt.name, joined, tt.wantSubstr)
		}
		hasWarning := strings.Contains(joined, "--beads rig")
		if hasWarning != tt.wantWarning {
			t.Errorf("%s: warning present = %v, want %v (%q)", tt.name, hasWarning, tt.wantWarning, joined)
		}
	}
}

func TestRegisterRig_PersistsBeadsMode(t *testing.T) {
	t.Parallel()
	root, rigsConfig := setupTestTown(t)
	rigPath := filepath.Join(root, "adopted")
	if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	// No config.json yet: --beads rig must create one carrying the mode.
	result, err := manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", BeadsPrefix: "ad", BeadsMode: "rig", Force: true})
	if err != nil {
		t.Fatalf("RegisterRig: %v", err)
	}
	if result.BeadsMode != config.RigBeadsModeRig {
		t.Fatalf("result.BeadsMode = %q, want rig", result.BeadsMode)
	}
	if !config.RigUsesOwnBeads(rigPath) {
		t.Fatal("config.json must record beads.mode=rig after adopt --beads rig")
	}
	cfg, err := LoadRigConfig(rigPath)
	if err != nil || cfg.Beads == nil || cfg.Beads.Prefix != "ad" {
		t.Fatalf("config.json beads = %+v (%v), want prefix ad", cfg, err)
	}
	if entry := manager.config.Rigs["adopted"]; entry.BeadsConfig == nil || entry.BeadsConfig.Mode != config.RigBeadsModeRig {
		t.Fatalf("rigs.json entry beads = %+v, want mode rig", entry.BeadsConfig)
	}
	if _, tracked := beads.TrackedRigBeadsDir(rigPath); tracked {
		t.Fatal("tracked mayor/rig/.beads must not count after adopt --beads rig")
	}

	// Re-adopt without --beads keeps the recorded mode.
	delete(manager.config.Rigs, "adopted")
	result, err = manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", Force: true})
	if err != nil {
		t.Fatalf("RegisterRig (re-adopt): %v", err)
	}
	if result.BeadsMode != config.RigBeadsModeRig || !config.RigUsesOwnBeads(rigPath) {
		t.Fatalf("re-adopt without --beads must keep mode rig, got %q", result.BeadsMode)
	}

	// Existing config.json in auto mode: --beads rig switches the mode and
	// keeps the recorded prefix and other fields.
	if err := manager.saveRigConfig(rigPath, &RigConfig{Type: "rig", Version: 1, Name: "adopted", GitURL: "file:///x", DefaultBranch: "dev", Beads: &BeadsConfig{Prefix: "keep", Mode: config.RigBeadsModeAuto}}); err != nil {
		t.Fatal(err)
	}
	delete(manager.config.Rigs, "adopted")
	if _, err := manager.RegisterRig(RegisterRigOptions{Name: "adopted", BeadsMode: "rig", Force: true}); err != nil {
		t.Fatalf("RegisterRig (switch to rig): %v", err)
	}
	cfg, err = LoadRigConfig(rigPath)
	if err != nil || cfg.Beads == nil || cfg.Beads.Mode != config.RigBeadsModeRig || cfg.Beads.Prefix != "keep" || cfg.DefaultBranch != "dev" {
		t.Fatalf("config.json after switch = %+v (%v), want mode rig, prefix keep, branch dev", cfg, err)
	}

	// Invalid mode is rejected.
	delete(manager.config.Rigs, "adopted")
	if _, err := manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", BeadsMode: "bogus", Force: true}); err == nil {
		t.Fatal("RegisterRig must reject an unknown beads mode")
	}
}

func TestAddRig_RejectsUnknownBeadsMode(t *testing.T) {
	t.Parallel()
	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))
	_, err := manager.AddRig(AddRigOptions{Name: "r1", GitURL: "file:///nonexistent", BeadsMode: "mayor", SkipDoltCheck: true})
	if err == nil || !strings.Contains(err.Error(), "beads mode") {
		t.Fatalf("AddRig with unknown beads mode: err = %v, want beads mode error", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "r1")); !os.IsNotExist(statErr) {
		t.Fatal("invalid beads mode must fail before creating the rig directory")
	}
}
