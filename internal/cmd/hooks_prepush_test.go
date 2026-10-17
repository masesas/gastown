package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/pushguard"
)

const testZeroSHA = "0000000000000000000000000000000000000000"

// prePushFixture is a remote (bare) plus a local clone with:
//   - main (pushed), base commit touching apps/api/ and docs/;
//   - integration/epic-a (pushed): one commit touching apps/api/epic.go;
//   - local "land" = main merged with integration/epic-a (not pushed);
//   - local "bad" = main + a commit touching infra/secret.tf (not pushed);
//   - local "good" = main + a commit touching apps/api/ok.go (not pushed).
type prePushFixture struct {
	remote, repo        string
	mainSHA, epicSHA    string
	land, bad, good     string
	integrationOnTopBad string // integration/epic-a + commit touching infra/
}

func newPrePushFixture(t *testing.T) prePushFixture {
	t.Helper()
	f := prePushFixture{remote: t.TempDir(), repo: t.TempDir()}
	g := func(args ...string) string {
		t.Helper()
		return strings.TrimSpace(runGitForMQSubmitTest(t, f.repo, args...))
	}
	runGitForMQSubmitTest(t, f.remote, "init", "--bare")
	g("init")
	g("config", "user.email", "test@example.com")
	g("config", "user.name", "Test User")
	g("remote", "add", "origin", f.remote)
	writeMQScopeFile(t, f.repo, "apps/api/main.go", "v1\n")
	writeMQScopeFile(t, f.repo, "docs/guide.md", "guide\n")
	g("add", "-A")
	g("commit", "-m", "base")
	g("branch", "-M", "main")
	g("push", "-u", "origin", "main")
	f.mainSHA = g("rev-parse", "HEAD")

	g("checkout", "-b", "integration/epic-a")
	writeMQScopeFile(t, f.repo, "apps/api/epic.go", "epic\n")
	g("add", "-A")
	g("commit", "-m", "epic")
	g("push", "-u", "origin", "integration/epic-a")
	f.epicSHA = g("rev-parse", "HEAD")
	writeMQScopeFile(t, f.repo, "infra/secret.tf", "x\n")
	g("add", "-A")
	g("commit", "-m", "integration bad")
	f.integrationOnTopBad = g("rev-parse", "HEAD")

	g("checkout", "-b", "land", "main")
	g("merge", "--no-ff", "-m", "land", f.epicSHA)
	f.land = g("rev-parse", "HEAD")

	g("checkout", "-b", "bad", "main")
	writeMQScopeFile(t, f.repo, "infra/secret.tf", "x\n")
	g("add", "-A")
	g("commit", "-m", "bad")
	f.bad = g("rev-parse", "HEAD")

	g("checkout", "-b", "good", "main")
	writeMQScopeFile(t, f.repo, "apps/api/ok.go", "ok\n")
	g("add", "-A")
	g("commit", "-m", "good")
	f.good = g("rev-parse", "HEAD")
	g("checkout", "main")
	return f
}

func prePushEnv(kv ...string) func(string) string {
	m := map[string]string{}
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return func(k string) string { return m[k] }
}

func prePushUpd(remoteRef, localSHA, remoteSHA string) prePushUpdate {
	return prePushUpdate{LocalRef: "refs/heads/x", LocalSHA: localSHA, RemoteRef: remoteRef, RemoteSHA: remoteSHA}
}

func TestPrePushGuard(t *testing.T) {
	f := newPrePushFixture(t)
	scoped := &prePushRig{name: "myrig", defaultBranch: "main", allowed: []string{"apps/api/**", "docs/**"}}
	autoLandOn := &prePushRig{name: "myrig", defaultBranch: "main", autoLand: autoLandSetting{enabled: true}}
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	broken := &prePushRig{name: "myrig", defaultBranch: "main", allowedErr: errors.New("merge scope: parsing settings")}
	agent := []string{"GT_ROLE", "refinery"}

	tests := []struct {
		name      string
		rig       *prePushRig
		env       []string
		url       string
		update    prePushUpdate
		wantErr   string // substring; "" = allowed
		forbidden string // substring that must not appear in the error
		unmanaged bool   // repository does not run the managed hook
	}{
		{name: "agent land auto_land=false rejected", rig: plain, env: agent,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "integration_branch_auto_land", forbidden: "GT_INTEGRATION_LAND"},
		{name: "agent land with GT_INTEGRATION_LAND=1 still rejected", rig: plain, env: append([]string{"GT_INTEGRATION_LAND", "1"}, agent...),
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "integration/epic-a", forbidden: "GT_INTEGRATION_LAND"},
		{name: "agent identified via BD_ACTOR", rig: plain, env: []string{"BD_ACTOR", "myrig/refinery"},
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "BD_ACTOR"},
		{name: "agent land auto_land=true via gt land allowed", rig: autoLandOn, env: append([]string{"GT_INTEGRATION_LAND", "1"}, agent...),
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA)},
		{name: "agent raw land auto_land=true rejected", rig: autoLandOn, env: agent,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "gt mq integration land", forbidden: "GT_INTEGRATION_LAND"},
		{name: "human land without env rejected with hint", rig: plain,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "GT_INTEGRATION_LAND=1"},
		{name: "human land with GT_INTEGRATION_LAND=1 allowed", rig: plain, env: []string{"GT_INTEGRATION_LAND", "1"},
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA)},
		{name: "human normal push allowed", rig: plain, update: prePushUpd("refs/heads/main", f.good, f.mainSHA)},
		{name: "human out-of-scope push not scope checked", rig: scoped, update: prePushUpd("refs/heads/main", f.bad, f.mainSHA)},
		{name: "agent in-scope push to main allowed", rig: scoped, env: agent, update: prePushUpd("refs/heads/main", f.good, f.mainSHA)},
		{name: "agent out-of-scope push to main rejected", rig: scoped, env: agent,
			update: prePushUpd("refs/heads/main", f.bad, f.mainSHA), wantErr: "infra/secret.tf"},
		{name: "agent out-of-scope push to integration rejected", rig: scoped, env: agent,
			update: prePushUpd("refs/heads/integration/epic-a", f.integrationOnTopBad, f.epicSHA), wantErr: "outside merge_queue.allowed_paths"},
		{name: "agent push to own integration branch is not a land", rig: plain, env: agent,
			update: prePushUpd("refs/heads/integration/epic-a", f.integrationOnTopBad, f.epicSHA)},
		{name: "agent new integration branch diffed from merge-base", rig: scoped, env: agent,
			update: prePushUpd("refs/heads/integration/epic-b", f.bad, testZeroSHA), wantErr: "infra/secret.tf"},
		{name: "agent new integration branch in scope allowed", rig: scoped, env: agent,
			update: prePushUpd("refs/heads/integration/epic-b", f.good, testZeroSHA)},
		{name: "agent nested land into another integration branch rejected", rig: plain, env: agent,
			update: prePushUpd("refs/heads/integration/parent", f.land, f.mainSHA), wantErr: "integration/epic-a"},
		{name: "agent polecat push skips all checks", rig: broken, env: agent,
			update: prePushUpd("refs/heads/polecat/nux/gt-1", f.bad, testZeroSHA)},
		{name: "agent other branch allowed", rig: scoped, env: agent, update: prePushUpd("refs/heads/feature/x", f.bad, testZeroSHA)},
		{name: "agent tag allowed", rig: broken, env: agent, update: prePushUpd("refs/tags/v1", f.bad, testZeroSHA)},
		{name: "agent deletion of unlanded integration branch refused (H1)", rig: plain, env: agent,
			update: prePushUpd("refs/heads/integration/epic-a", testZeroSHA, f.epicSHA), wantErr: "has not landed"},
		{name: "agent deletion of default branch refused (H1)", rig: plain, env: agent,
			update: prePushUpd("refs/heads/main", testZeroSHA, f.mainSHA), wantErr: "may not delete the default branch"},
		{name: "human deletion of integration branch allowed", rig: plain,
			update: prePushUpd("refs/heads/integration/epic-a", testZeroSHA, f.epicSHA)},
		{name: "agent deletion of other branch allowed", rig: broken, env: agent,
			update: prePushUpd("refs/heads/feature/x", testZeroSHA, f.epicSHA)},
		{name: "agent unknown default: master is a target too (M2)", rig: &prePushRig{name: "myrig", allowed: []string{"apps/api/**"}}, env: agent,
			update: prePushUpd("refs/heads/master", f.bad, f.mainSHA), wantErr: "infra/secret.tf"},
		{name: "human unknown default: master is not a target", rig: &prePushRig{name: "myrig", allowed: []string{"apps/api/**"}},
			update: prePushUpd("refs/heads/master", f.bad, f.mainSHA)},
		{name: "option-looking push URL refused (L1)", rig: plain, env: agent, url: "--upload-pack=touch /tmp/x",
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "invalid push remote"},
		{name: "agent unreadable scope fails closed", rig: broken, env: agent,
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "parsing settings"},
		{name: "agent outside a rig: target push refused (H2)", rig: nil, env: agent,
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "no rig was detected"},
		{name: "agent in unmanaged non-rig repo: plain target push allowed (N1)", rig: nil, env: agent, unmanaged: true,
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA)},
		{name: "agent in unmanaged non-rig repo: land still refused (N1)", rig: nil, env: agent, unmanaged: true,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "auto-land treated as off"},
		{name: "agent in unmanaged non-rig repo: non-fast-forward refused (N1)", rig: nil, env: agent, unmanaged: true,
			update: prePushUpd("refs/heads/main", f.good, f.bad), wantErr: "not a fast-forward"},
		{name: "agent non-fast-forward to main refused (B1)", rig: plain, env: agent,
			update: prePushUpd("refs/heads/main", f.good, f.bad), wantErr: "not a fast-forward"},
		{name: "agent rewind of integration branch refused (B1)", rig: plain, env: agent,
			update: prePushUpd("refs/heads/integration/epic-a", f.mainSHA, f.epicSHA), wantErr: "not a fast-forward"},
		{name: "human non-fast-forward to main not refused by the guard", rig: plain,
			update: prePushUpd("refs/heads/main", f.good, f.bad)},
		{name: "agent non-fast-forward to polecat branch allowed", rig: plain, env: agent,
			update: prePushUpd("refs/heads/polecat/nux", f.good, f.bad)},
		{name: "agent outside a rig: polecat push allowed", rig: nil, env: agent,
			update: prePushUpd("refs/heads/polecat/a", f.good, testZeroSHA)},
		{name: "agent unreachable remote fails closed", rig: plain, env: agent, url: filepath.Join(t.TempDir(), "missing.git"),
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "cannot list integration branches", forbidden: "GT_INTEGRATION_LAND"},
		{name: "human unreachable remote fails closed with hint", rig: plain, url: filepath.Join(t.TempDir(), "missing.git"),
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "GT_INTEGRATION_LAND=1"},
		{name: "agent unknown remote sha fails closed (scope)", rig: scoped, env: agent,
			update: prePushUpd("refs/heads/main", f.good, "1234567890123456789012345678901234567890"), wantErr: "fetch"},
		{name: "agent land over unknown remote sha fails closed", rig: plain, env: agent,
			update: prePushUpd("refs/heads/main", f.land, "1234567890123456789012345678901234567890"), wantErr: "fetch first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := tt.url
			if url == "" {
				url = f.remote
			}
			rig := tt.rig
			g := &prePushGuard{
				remote:  "origin",
				url:     url,
				getenv:  prePushEnv(tt.env...),
				git:     gitpkg.NewGit(f.repo),
				loadRig: func() (*prePushRig, error) { return rig, nil },
			}
			if tt.unmanaged {
				g.isManaged = func() bool { return false }
			}
			err := g.check([]prePushUpdate{tt.update})
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("want allowed, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want rejection containing %q, got allowed", tt.wantErr)
			}
			t.Logf("refused: %v", err)
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error %q does not contain %q", err, tt.wantErr)
			}
			if tt.forbidden != "" && strings.Contains(err.Error(), tt.forbidden) {
				t.Fatalf("error must not mention %q: %v", tt.forbidden, err)
			}
			if strings.Contains(err.Error(), f.remote) || strings.Contains(err.Error(), "upload-pack") {
				t.Fatalf("error leaks the push URL: %v", err)
			}
		})
	}
}

func TestPrePushGuardPolecatDoesNoRigIO(t *testing.T) {
	calls := 0
	g := &prePushGuard{
		remote: "origin",
		getenv: prePushEnv("GT_POLECAT", "nux"),
		loadRig: func() (*prePushRig, error) {
			calls++
			return nil, errors.New("must not be called")
		},
	}
	err := g.check([]prePushUpdate{prePushUpd("refs/heads/polecat/nux/gt-1", "abc", testZeroSHA)})
	if err != nil || calls != 0 {
		t.Fatalf("err=%v calls=%d; polecat pushes must not load the rig", err, calls)
	}
}

func TestPrePushGuardRigLoadErrorFailsClosedForAgents(t *testing.T) {
	load := func() (*prePushRig, error) { return nil, errors.New("rig settings unreadable") }
	agentGuard := &prePushGuard{remote: "origin", getenv: prePushEnv("GT_CREW", "dom"), loadRig: load}
	if err := agentGuard.check([]prePushUpdate{prePushUpd("refs/heads/main", "abc", "def")}); err == nil {
		t.Fatal("agent push must fail closed when the rig cannot be loaded")
	}
	f := newPrePushFixture(t)
	humanGuard := &prePushGuard{remote: "origin", url: f.remote, getenv: prePushEnv(), loadRig: load, git: gitpkg.NewGit(f.repo)}
	if err := humanGuard.check([]prePushUpdate{prePushUpd("refs/heads/main", f.good, f.mainSHA)}); err != nil {
		t.Fatalf("human push should fall back to git defaults: %v", err)
	}
}

func TestParsePrePushUpdates(t *testing.T) {
	in := "refs/heads/main 1111 refs/heads/main 2222\n\nrefs/tags/v1 3333 refs/tags/v1 " + testZeroSHA + "\n"
	got, err := parsePrePushUpdates(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].RemoteSHA != "2222" || got[1].RemoteRef != "refs/tags/v1" {
		t.Fatalf("got %+v", got)
	}
	if _, err := parsePrePushUpdates(strings.NewReader("refs/heads/main 1111\n")); err == nil {
		t.Fatal("malformed line must fail closed")
	}
}

func TestResolvePrePushRig(t *testing.T) {
	town := t.TempDir()
	rigPath := filepath.Join(town, "myrig")
	writeMQScopeRigSettings(t, rigPath, map[string]any{
		"integration_branch_auto_land": true,
		"allowed_paths":                []string{"apps/**"},
	})
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"type":"rig","default_branch":"develop"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(rigPath, "refinery", "rig")
	if err := os.MkdirAll(cwd, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := resolvePrePushRig(town, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil || r.name != "myrig" || r.defaultBranch != "develop" || !r.autoLand.enabled ||
		len(r.allowed) != 1 || r.allowedErr != nil {
		t.Fatalf("got %+v", r)
	}

	if r, err := resolvePrePushRig(town, town); err != nil || r != nil {
		t.Fatalf("town root is not a rig: r=%+v err=%v", r, err)
	}
	if r, err := resolvePrePushRig("", cwd); err != nil || r != nil {
		t.Fatalf("no town: r=%+v err=%v", r, err)
	}
	if r, err := resolvePrePushRig(town, filepath.Join(town, "deacon", "x")); err != nil || r != nil {
		t.Fatalf("a town dir without config.json is not a rig: r=%+v err=%v", r, err)
	}
	if r, err := resolvePrePushRig(town, filepath.Join(town, "mayor")); err != nil || r != nil {
		t.Fatalf("mayor dir is not a rig: r=%+v err=%v", r, err)
	}

	// A malformed settings file fails closed for scope and auto-land.
	if err := os.WriteFile(filepath.Join(rigPath, "settings", "config.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err = resolvePrePushRig(town, cwd)
	if err != nil {
		t.Fatal(err)
	}
	if r.allowedErr == nil || r.autoLand.enabled {
		t.Fatalf("malformed settings must fail closed: %+v", r)
	}
}

func TestRunHooksPrePushCommand(t *testing.T) {
	f := newPrePushFixture(t)
	t.Chdir(f.repo)
	for _, k := range agentSessionEnvVars {
		t.Setenv(k, "")
	}
	t.Setenv("GT_INTEGRATION_LAND", "")
	run := func(stdin string, args ...string) error {
		hooksPrePushCmd.SetIn(strings.NewReader(stdin))
		defer hooksPrePushCmd.SetIn(nil)
		return runHooksPrePush(hooksPrePushCmd, args)
	}
	if err := run("refs/heads/main "+f.good+" refs/heads/main "+f.mainSHA+"\n", "origin", f.remote); err != nil {
		t.Fatalf("human normal push: %v", err)
	}
	if err := run("refs/heads/main "+f.land+" refs/heads/main "+f.mainSHA+"\n", "origin"); err == nil {
		t.Fatal("human land without GT_INTEGRATION_LAND must be refused")
	}
	t.Setenv("GT_ROLE", "refinery")
	t.Setenv("GT_INTEGRATION_LAND", "1")
	if err := run("refs/heads/main "+f.land+" refs/heads/main "+f.mainSHA+"\n", "origin", f.remote); err == nil {
		t.Fatal("agent land outside a rig must be refused")
	}
	if err := run("garbage\n", "origin", f.remote); err == nil {
		t.Fatal("malformed input must be refused")
	}
}

func newGuard(f prePushFixture, r *prePushRig, env ...string) *prePushGuard {
	return &prePushGuard{
		remote:  "origin",
		url:     f.remote,
		getenv:  prePushEnv(env...),
		git:     gitpkg.NewGit(f.repo),
		loadRig: func() (*prePushRig, error) { return r, nil },
	}
}

// H1: deleting the integration branch on the remote must not hide a later
// land; the local tracking ref still names its tip.
func TestPrePushGuardDeleteThenLandStillDetected(t *testing.T) {
	f := newPrePushFixture(t)
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	// Delete the branch on the remote directly (as a human would, no hook).
	// Pushing by URL keeps the tracking ref: it alone must catch the land.
	runGitForMQSubmitTest(t, f.repo, "branch", "-m", "integration/epic-a", "epic-renamed")
	runGitForMQSubmitTest(t, f.repo, "push", f.remote, ":refs/heads/integration/epic-a")
	land := []prePushUpdate{prePushUpd("refs/heads/main", f.land, f.mainSHA)}
	err := newGuard(f, plain, "GT_ROLE", "refinery").check(land)
	if err == nil || !strings.Contains(err.Error(), "integration/epic-a") {
		t.Fatalf("land after remote deletion (tracking ref) must be refused, got %v", err)
	}
	// Deleting via the remote name also prunes the tracking ref; the local
	// integration branch must then catch it.
	runGitForMQSubmitTest(t, f.repo, "update-ref", "-d", "refs/remotes/origin/integration/epic-a")
	runGitForMQSubmitTest(t, f.repo, "update-ref", "refs/heads/integration/epic-a", f.epicSHA)
	err = newGuard(f, plain, "GT_ROLE", "refinery").check(land)
	if err == nil || !strings.Contains(err.Error(), "integration/epic-a") {
		t.Fatalf("land after remote deletion (local branch) must be refused, got %v", err)
	}
}

// H1: an agent may delete an integration branch once it has landed.
func TestPrePushGuardAgentDeletesLandedIntegrationBranch(t *testing.T) {
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "push", f.remote, f.land+":refs/heads/main")
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	err := newGuard(f, plain, "GT_ROLE", "refinery").check([]prePushUpdate{
		prePushUpd("refs/heads/integration/epic-a", testZeroSHA, f.epicSHA)})
	if err != nil {
		t.Fatalf("deleting a landed integration branch should pass: %v", err)
	}
}

// L3: the new-branch base comes from the remote, not a movable local ref.
func TestPrePushGuardNewBranchBaseIgnoresLocalTrackingRef(t *testing.T) {
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "update-ref", "refs/remotes/origin/main", f.bad)
	scoped := &prePushRig{name: "myrig", defaultBranch: "main", allowed: []string{"apps/api/**", "docs/**"}}
	err := newGuard(f, scoped, "GT_ROLE", "refinery").check([]prePushUpdate{
		prePushUpd("refs/heads/integration/epic-b", f.bad, testZeroSHA)})
	if err == nil || !strings.Contains(err.Error(), "infra/secret.tf") {
		t.Fatalf("moved local tracking ref must not hide out-of-scope paths, got %v", err)
	}
}

// H2: a cwd reached through a symlink still resolves the rig, so the scope
// check runs; outside any rig an agent push to a target is refused.
func TestRunHooksPrePushSymlinkCwd(t *testing.T) {
	f := newPrePushFixture(t)
	town := t.TempDir()
	if err := os.MkdirAll(filepath.Join(town, "mayor"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(town, "mayor", "town.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	rigPath := filepath.Join(town, "myrig")
	writeMQScopeRigSettings(t, rigPath, map[string]any{"allowed_paths": []string{"apps/api/**", "docs/**"}})
	if err := os.WriteFile(filepath.Join(rigPath, "config.json"), []byte(`{"type":"rig","default_branch":"main"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	crew := filepath.Join(rigPath, "crew", "dom")
	runGitForMQSubmitTest(t, town, "clone", "-q", f.repo, crew)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(crew, link); err != nil {
		t.Fatal(err)
	}
	t.Chdir(link)
	t.Setenv("PWD", link)
	for _, k := range agentSessionEnvVars {
		t.Setenv(k, "")
	}
	t.Setenv("GT_ROLE", "crew")
	run := func(stdin string) error {
		hooksPrePushCmd.SetIn(strings.NewReader(stdin))
		defer hooksPrePushCmd.SetIn(nil)
		return runHooksPrePush(hooksPrePushCmd, []string{"origin", f.remote})
	}
	err := run("refs/heads/main " + f.bad + " refs/heads/main " + f.mainSHA + "\n")
	if err == nil || !strings.Contains(err.Error(), "infra/secret.tf") {
		t.Fatalf("scope must be enforced through a symlinked cwd, got %v", err)
	}
	if err := run("refs/heads/main " + f.good + " refs/heads/main " + f.mainSHA + "\n"); err != nil {
		t.Fatalf("in-scope push through symlink should pass: %v", err)
	}

	// Outside any town, unmanaged repo (e.g. the Gas Town source repo):
	// a plain agent push to main passes; a land would still be refused.
	t.Chdir(f.repo)
	if err := run("refs/heads/main " + f.good + " refs/heads/main " + f.mainSHA + "\n"); err != nil {
		t.Fatalf("agent push from an unmanaged non-rig repo: %v", err)
	}
	// Same repo with the managed hook installed (a rig repo whose rig cannot
	// be detected): refused.
	if _, err := pushguard.Install(f.repo); err != nil {
		t.Fatal(err)
	}
	if err := run("refs/heads/main " + f.good + " refs/heads/main " + f.mainSHA + "\n"); err == nil ||
		!strings.Contains(err.Error(), "no rig was detected") {
		t.Fatalf("agent push from a managed repo without rig must be refused, got %v", err)
	}
	if err := run("refs/heads/polecat/x " + f.good + " refs/heads/polecat/x " + testZeroSHA + "\n"); err != nil {
		t.Fatalf("polecat push outside a rig: %v", err)
	}
}
