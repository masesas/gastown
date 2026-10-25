package rig

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
)

// bdShapedGitignore mirrors the .beads/.gitignore that bd writes (as tracked
// by real projects such as oto-rent-pro), including the "redirect" entry.
const bdShapedGitignore = `# Dolt database (managed by Dolt, not git)
dolt/
dolt-access.lock

# Runtime files
bd.sock
bd.sock.startlock
sync-state.json
last-touched

# Local version tracking (prevents upgrade notification spam after git ops)
.local_version

# Worktree redirect file (contains relative path to main repo's .beads/)
# Must not be committed as paths would be wrong in other clones
redirect

# Sync state (local-only, per-machine)
.sync.lock
export-state/

# Dolt server management (auto-started by bd)
dolt-server.pid
dolt-server.log
dolt-server.lock
dolt-server.port

# Legacy files (from pre-Dolt versions)
*.db
*.db?*
bd.db
daemon.*
*.lock
`

// gitignoreWithoutRedirect is a project .beads/.gitignore that does NOT
// ignore "redirect"; the mayor clone must stay clean anyway.
const gitignoreWithoutRedirect = "dolt/\n*.db\n"

const projectSyncRemote = "sync.remote"

func TestRigConfig_BeadsModeRoundTrip(t *testing.T) {
	t.Parallel()
	rigPath := filepath.Join(t.TempDir(), "r")
	if err := os.MkdirAll(rigPath, 0755); err != nil {
		t.Fatal(err)
	}
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

// installLoggingFakeBD puts a bd shim on PATH that logs "<cmd> <cwd> <args>"
// and answers the subcommands AddRig uses. Returns the log path.
func installLoggingFakeBD(t *testing.T) string {
	t.Helper()
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
	return cmdLog
}

// makeProjectRepo creates a committed git repo; beadsFiles (may be nil) are
// written under .beads/.
func makeProjectRepo(t *testing.T, beadsFiles map[string]string) string {
	t.Helper()
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("project\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if len(beadsFiles) > 0 {
		if err := os.MkdirAll(filepath.Join(repoDir, ".beads"), 0755); err != nil {
			t.Fatal(err)
		}
		for name, content := range beadsFiles {
			if err := os.WriteFile(filepath.Join(repoDir, ".beads", name), []byte(content), 0644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, args := range [][]string{
		{"git", "init", "--initial-branch=main", repoDir},
		{"git", "-C", repoDir, "config", "user.email", "test@test.com"},
		{"git", "-C", repoDir, "config", "user.name", "Test User"},
		{"git", "-C", repoDir, "add", "."},
		{"git", "-C", repoDir, "commit", "-m", "project"},
	} {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}
	return repoDir
}

// dirSnapshot maps each regular file under dir (relative path) to
// "<size>:<sha256>".
func dirSnapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		snap[filepath.ToSlash(rel)] = strconv.Itoa(len(data)) + ":" + hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot %s: %v", dir, err)
	}
	return snap
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestAddRig_RigMode_LeavesTrackedBeadsUntouched(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based bd shim not reliable on Windows CI")
	}
	for _, tc := range []struct {
		name      string
		gitignore string
	}{
		{name: "bd-shaped gitignore", gitignore: bdShapedGitignore},
		{name: "gitignore without redirect", gitignore: gitignoreWithoutRedirect},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertRigModeAddLeavesProjectBeadsUntouched(t, tc.gitignore)
		})
	}
}

func assertRigModeAddLeavesProjectBeadsUntouched(t *testing.T, gitignore string) {
	cmdLog := installLoggingFakeBD(t)

	// A project repo that tracks its own beads with a sync.remote (the
	// oto-rent-pro shape).
	trackedConfig := "issue-prefix: oto\n" + projectSyncRemote + ": \"git+ssh://git@example.invalid/project.git\"\n"
	files := map[string]string{
		"config.yaml":   trackedConfig,
		"metadata.json": `{"database":"dolt","backend":"dolt","dolt_database":"oto"}` + "\n",
		".gitignore":    gitignore,
		"issues.jsonl":  "{\"id\":\"oto-1\"}\n",
		"README.md":     "beads\n",
	}
	repoDir := makeProjectRepo(t, files)
	sourceSnap := dirSnapshot(t, filepath.Join(repoDir, ".beads"))

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

	// 1. config.json and rigs.json persist the mode and the rig's own prefix.
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

	// 2. Full listing of mayor/rig/.beads: identical to the project's tracked
	//    .beads/ plus exactly one new file, redirect = "../../.beads\n".
	afterSnap := dirSnapshot(t, filepath.Join(mayorRig, ".beads"))
	for _, name := range sortedKeys(sourceSnap) {
		if afterSnap[name] != sourceSnap[name] {
			t.Errorf("tracked .beads/%s changed in mayor clone: %q -> %q", name, sourceSnap[name], afterSnap[name])
		}
	}
	var added []string
	for _, name := range sortedKeys(afterSnap) {
		if _, ok := sourceSnap[name]; !ok {
			added = append(added, name)
		}
	}
	if len(added) != 1 || added[0] != "redirect" {
		t.Fatalf("mayor/rig/.beads gained %v, want exactly [redirect]", added)
	}
	if got := readFileOrEmpty(t, filepath.Join(mayorRig, ".beads", "redirect")); got != "../../.beads\n" {
		t.Fatalf("mayor clone redirect = %q, want %q", got, "../../.beads\n")
	}

	// 3. The mayor clone is git-clean (whatever the project's .gitignore) and
	//    nothing is hidden with skip-worktree.
	out, err := exec.Command("git", "-C", mayorRig, "status", "--porcelain", "--untracked-files=all").CombinedOutput()
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

	// 4. Rig beads are self-contained; mayor clone + refinery resolve to them;
	//    the project's sync.remote stays in the project and out of the rig.
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
	rigConfigYAML := readFileOrEmpty(t, filepath.Join(rigBeads, "config.yaml"))
	if rigConfigYAML == "" {
		t.Fatal("rig beads config.yaml must exist")
	}
	if strings.Contains(rigConfigYAML, projectSyncRemote) {
		t.Fatalf("rig beads config must not contain %s:\n%s", projectSyncRemote, rigConfigYAML)
	}
	if !strings.Contains(readFileOrEmpty(t, filepath.Join(mayorRig, ".beads", "config.yaml")), projectSyncRemote) {
		t.Fatal("the project's tracked config.yaml must still carry its sync.remote")
	}

	// 5. No bd command ran inside the mayor clone, and no destroy-token was used.
	log := readFileOrEmpty(t, cmdLog)
	if strings.Contains(log, "--destroy-token") || strings.Contains(log, "--discard-remote") {
		t.Fatalf("rig mode must never pass destroy/discard flags; log:\n%s", log)
	}
	for _, line := range strings.Split(log, "\n") {
		if strings.HasPrefix(line, "init ") && strings.Contains(line, mayorRig) {
			t.Fatalf("bd init ran inside the mayor clone: %q", line)
		}
	}

	// 6. Route points at the rig root, not mayor/rig.
	routes := readFileOrEmpty(t, filepath.Join(root, ".beads", "routes.jsonl"))
	if !strings.Contains(routes, `"path":"otorig"`) || strings.Contains(routes, "otorig/mayor/rig") {
		t.Fatalf("route for rig mode must be the rig root; routes.jsonl:\n%s", routes)
	}
}

func TestAddRig_AutoMode_DoesNotPersistMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-based bd shim not reliable on Windows CI")
	}
	installLoggingFakeBD(t)
	repoDir := makeProjectRepo(t, nil)

	root, rigsConfig := setupTestTown(t)
	manager := NewManager(root, rigsConfig, git.NewGit(root))
	if _, err := manager.AddRig(AddRigOptions{Name: "plain", GitURL: repoDir, BeadsPrefix: "pl", SkipDoltCheck: true}); err != nil {
		t.Fatalf("AddRig: %v", err)
	}
	raw := readFileOrEmpty(t, filepath.Join(root, "plain", "config.json"))
	if strings.Contains(raw, `"mode"`) {
		t.Fatalf("auto mode must not write beads.mode to config.json:\n%s", raw)
	}
	reg := readFileOrEmpty(t, filepath.Join(root, "mayor", "rigs.json"))
	if strings.Contains(reg, `"mode"`) {
		t.Fatalf("auto mode must not write beads.mode to rigs.json:\n%s", reg)
	}
}

func TestBeadsModeNotice(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mode       string
		tracked    bool
		syncRemote bool
		want       []string // substrings, one per expected line
	}{
		{name: "rig tracked", mode: config.RigBeadsModeRig, tracked: true, syncRemote: true, want: []string{"left untouched"}},
		{name: "rig untracked", mode: config.RigBeadsModeRig, want: []string{"Beads mode: rig"}},
		{name: "auto untracked prints nothing", mode: config.RigBeadsModeAuto},
		{name: "auto tracked prints nothing", mode: config.RigBeadsModeAuto, tracked: true},
		{name: "auto tracked sync.remote warns once", mode: config.RigBeadsModeAuto, tracked: true, syncRemote: true, want: []string{"--beads rig"}},
	}
	for _, tt := range tests {
		lines := beadsModeNotice(tt.mode, tt.tracked, tt.syncRemote)
		if len(lines) != len(tt.want) {
			t.Errorf("%s: got %d lines %q, want %d", tt.name, len(lines), lines, len(tt.want))
			continue
		}
		for i, sub := range tt.want {
			if !strings.Contains(lines[i], sub) {
				t.Errorf("%s: line %q missing %q", tt.name, lines[i], sub)
			}
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

	// --beads auto (or no flag) on a directory without config.json writes nothing.
	result, err := manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", BeadsPrefix: "ad", BeadsMode: "auto", Force: true})
	if err != nil {
		t.Fatalf("RegisterRig(auto): %v", err)
	}
	if result.BeadsMode != config.RigBeadsModeAuto {
		t.Fatalf("result.BeadsMode = %q, want auto", result.BeadsMode)
	}
	if _, err := os.Stat(filepath.Join(rigPath, "config.json")); !os.IsNotExist(err) {
		t.Fatal("adopt --beads auto must not create config.json")
	}
	if entry := manager.config.Rigs["adopted"]; entry.BeadsConfig == nil || entry.BeadsConfig.Mode != "" {
		t.Fatalf("rigs.json entry must not record auto, got %+v", entry.BeadsConfig)
	}

	// No config.json yet: --beads rig must create one carrying the mode.
	delete(manager.config.Rigs, "adopted")
	result, err = manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", BeadsPrefix: "ad", BeadsMode: "rig", Force: true})
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

	// rig -> auto is a manual migration.
	delete(manager.config.Rigs, "adopted")
	if _, err := manager.RegisterRig(RegisterRigOptions{Name: "adopted", GitURL: "file:///x", BeadsMode: "auto", Force: true}); err == nil || !strings.Contains(err.Error(), "migrasi rig→auto manual") {
		t.Fatalf("adopt --beads auto on a rig-mode rig: err = %v, want manual-migration error", err)
	}

	// Existing config.json without a mode: --beads rig switches the mode and
	// keeps the recorded prefix and other fields.
	if err := manager.saveRigConfig(rigPath, &RigConfig{Type: "rig", Version: 1, Name: "adopted", GitURL: "file:///x", DefaultBranch: "dev", Beads: &BeadsConfig{Prefix: "keep"}}); err != nil {
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

func TestRegisterRig_RejectsAutoToRigWithRigRedirect(t *testing.T) {
	t.Parallel()
	root, rigsConfig := setupTestTown(t)
	rigPath := filepath.Join(root, "legacy")
	if err := os.MkdirAll(filepath.Join(rigPath, "mayor", "rig", ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, ".beads"), 0755); err != nil {
		t.Fatal(err)
	}
	redirect := filepath.Join(rigPath, ".beads", "redirect")
	if err := os.WriteFile(redirect, []byte("mayor/rig/.beads\n"), 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(root, rigsConfig, git.NewGit(root))

	_, err := manager.RegisterRig(RegisterRigOptions{Name: "legacy", GitURL: "file:///x", BeadsPrefix: "lg", BeadsMode: "rig", Force: true})
	if err == nil || !strings.Contains(err.Error(), "migrasi auto→rig manual") || !strings.Contains(err.Error(), redirect) {
		t.Fatalf("adopt --beads rig on an auto rig with redirect: err = %v, want manual-migration error naming %s", err, redirect)
	}
	if _, statErr := os.Stat(filepath.Join(rigPath, "config.json")); !os.IsNotExist(statErr) {
		t.Fatal("rejected adopt must not write config.json")
	}
	if _, ok := manager.config.Rigs["legacy"]; ok {
		t.Fatal("rejected adopt must not register the rig")
	}
	if got := readFileOrEmpty(t, redirect); got != "mayor/rig/.beads\n" {
		t.Fatalf("rejected adopt must leave the redirect alone, got %q", got)
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
