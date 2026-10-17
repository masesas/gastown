package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gt-pd1c.78 tahap 2: the Go guard and the bash fallback (.githooks/pre-push,
// no gt on PATH) agree on archives and archived tips whenever the merge queue
// gives the Go guard nothing more (no MR from the branch). The fallback is
// stricter only where the Go guard would need the queue (archive creation).
func TestArchiveRulesBashParity(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	hook, err := filepath.Abs(filepath.Join("..", "..", ".githooks", "pre-push"))
	if err != nil {
		t.Fatal(err)
	}
	f := newPrePushFixture(t)
	runGitForMQSubmitTest(t, f.repo, "remote", "set-head", "origin", "main")
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.good+":refs/heads/polecat/nux")
	runGitForMQSubmitTest(t, f.repo, "push", "-q", "origin", f.bad+":refs/heads/polecat/max")
	archiveTip(t, f, "polecat/nux", f.good)
	archive := "refs/gt/archive/polecat/nux/1760000000-" + f.good[:12]
	nogt := t.TempDir()
	for _, tool := range []string{"git", "sed", "awk", "cat"} {
		p, err := exec.LookPath(tool)
		if err != nil {
			t.Skipf("%s not available", tool)
		}
		if err := os.Symlink(p, filepath.Join(nogt, tool)); err != nil {
			t.Fatal(err)
		}
	}
	runBash := func(env []string, line string) bool {
		cmd := exec.Command(bash, hook, "origin")
		cmd.Dir = f.repo
		cmd.Env = []string{"PATH=" + nogt, "HOME=" + t.TempDir()}
		for i := 0; i+1 < len(env); i += 2 {
			cmd.Env = append(cmd.Env, env[i]+"="+env[i+1])
		}
		cmd.Stdin = strings.NewReader(line + "\n")
		return cmd.Run() == nil
	}
	plain := &prePushRig{name: "myrig", defaultBranch: "main"}
	runGo := func(env []string, line string) bool {
		updates, err := parsePrePushUpdates(strings.NewReader(line + "\n"))
		if err != nil {
			t.Fatal(err)
		}
		return coordinatorGuard(f, plain, mqWith(), env...).check(updates) == nil
	}
	witness := []string{"GT_ROLE", "gastown/witness"}
	mayor := []string{"GT_ROLE", "mayor"}
	crew := []string{"GT_ROLE", "gastown/crew/max"}
	rows := []struct {
		name  string
		env   []string
		line  string
		allow bool
	}{
		{"witness deletes an archived tip", witness, "(delete) " + testZeroSHA + " refs/heads/polecat/nux " + f.good, true},
		{"witness deletes an unarchived tip", witness, "(delete) " + testZeroSHA + " refs/heads/polecat/max " + f.bad, false},
		{"witness rewrites an archived tip", witness, "HEAD " + f.mainSHA + " refs/heads/polecat/nux " + f.good, true},
		{"witness rewrites an unarchived tip", witness, "HEAD " + f.mainSHA + " refs/heads/polecat/max " + f.bad, false},
		{"mayor deletes an archive", mayor, "(delete) " + testZeroSHA + " " + archive + " " + f.good, false},
		{"witness rewrites an archive", witness, "HEAD " + f.bad + " " + archive + " " + f.good, false},
		{"mayor archives work without an MR", mayor, "HEAD " + f.bad + " refs/gt/archive/polecat/max/1760000000-" + f.bad[:12] + " " + testZeroSHA, false},
		{"crew prunes an archive", crew, "(delete) " + testZeroSHA + " " + archive + " " + f.good, true},
		{"human prunes an archive", nil, "(delete) " + testZeroSHA + " " + archive + " " + f.good, true},
		{"human archives anything", nil, "HEAD " + f.bad + " refs/gt/archive/polecat/max/1760000000-" + f.bad[:12] + " " + testZeroSHA, true},
	}
	for _, r := range rows {
		if got := runBash(r.env, r.line); got != r.allow {
			t.Errorf("bash, %s: allowed=%v, want %v", r.name, got, r.allow)
		}
		if got := runGo(r.env, r.line); got != r.allow {
			t.Errorf("go, %s: allowed=%v, want %v", r.name, got, r.allow)
		}
	}
}
