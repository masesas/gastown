package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
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
	}{
		{name: "agent land auto_land=false rejected", rig: plain, env: agent,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "integration_branch_auto_land", forbidden: "GT_INTEGRATION_LAND"},
		{name: "agent land with GT_INTEGRATION_LAND=1 still rejected", rig: plain, env: append([]string{"GT_INTEGRATION_LAND", "1"}, agent...),
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "integration/epic-a", forbidden: "GT_INTEGRATION_LAND"},
		{name: "agent identified via BD_ACTOR", rig: plain, env: []string{"BD_ACTOR", "myrig/polecats/nux"},
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
		{name: "agent deletion allowed", rig: broken, env: agent, update: prePushUpd("refs/heads/integration/epic-a", testZeroSHA, f.epicSHA)},
		{name: "agent unreadable scope fails closed", rig: broken, env: agent,
			update: prePushUpd("refs/heads/main", f.good, f.mainSHA), wantErr: "parsing settings"},
		{name: "agent outside a rig: land rejected", rig: nil, env: agent,
			update: prePushUpd("refs/heads/main", f.land, f.mainSHA), wantErr: "auto-land treated as off"},
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
			if strings.Contains(err.Error(), f.remote) {
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
