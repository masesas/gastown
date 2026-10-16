package pushguard

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runScript executes the managed hook from hooksDir with the given PATH and
// stdin, as git would (argv: remote url).
func runScript(t *testing.T, hooksDir, path, stdin string) (string, error) {
	t.Helper()
	cmd := exec.Command("/bin/sh", filepath.Join(hooksDir, "pre-push"), "origin", "https://example.invalid/r.git")
	cmd.Env = []string{"PATH=" + path, "HOME=" + t.TempDir()}
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func scriptFixture(t *testing.T) (hooksDir, binDir, logDir string) {
	t.Helper()
	root := t.TempDir()
	hooksDir = filepath.Join(root, "hooks")
	binDir = filepath.Join(root, "bin")
	logDir = filepath.Join(root, "log")
	for _, d := range []string{hooksDir, binDir, logDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeExec(t, filepath.Join(hooksDir, "pre-push"), Script())
	return hooksDir, binDir, logDir
}

// fakeGT writes a gt stub that records argv and stdin and exits with code.
func fakeGT(t *testing.T, binDir, logDir string, code int) {
	t.Helper()
	writeExec(t, filepath.Join(binDir, "gt"), "#!/bin/sh\n"+
		"echo \"$@\" > "+filepath.Join(logDir, "gt.args")+"\n"+
		"cat > "+filepath.Join(logDir, "gt.stdin")+"\n"+
		"exit "+string(rune('0'+code))+"\n")
}

const sysPath = "/usr/bin:/bin"

func TestScriptFailsClosedWithoutGT(t *testing.T) {
	hooksDir, binDir, _ := scriptFixture(t)
	out, err := runScript(t, hooksDir, binDir+":"+sysPath, "refs/heads/main a refs/heads/main b\n")
	if err == nil {
		t.Fatal("hook must reject the push when gt is not on PATH")
	}
	if !strings.Contains(out, "gt") || !strings.Contains(out, "PATH") {
		t.Fatalf("unclear message: %q", out)
	}
}

func TestScriptForwardsToGTAndChains(t *testing.T) {
	hooksDir, binDir, logDir := scriptFixture(t)
	fakeGT(t, binDir, logDir, 0)
	writeExec(t, filepath.Join(hooksDir, ChainedHookName), "#!/bin/sh\n"+
		"echo \"$@\" > "+filepath.Join(logDir, "chain.args")+"\n"+
		"cat > "+filepath.Join(logDir, "chain.stdin")+"\n")
	in := "refs/heads/main 1111 refs/heads/main 2222\nrefs/heads/x 3333 refs/heads/x 4444\n"
	if out, err := runScript(t, hooksDir, binDir+":"+sysPath, in); err != nil {
		t.Fatalf("push should pass: %v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(logDir, "gt.args")); got != "hooks pre-push origin https://example.invalid/r.git\n" {
		t.Fatalf("gt argv = %q", got)
	}
	if got := readFile(t, filepath.Join(logDir, "gt.stdin")); got != in {
		t.Fatalf("gt stdin = %q", got)
	}
	if got := readFile(t, filepath.Join(logDir, "chain.args")); got != "origin https://example.invalid/r.git\n" {
		t.Fatalf("chained argv = %q", got)
	}
	if got := readFile(t, filepath.Join(logDir, "chain.stdin")); got != in {
		t.Fatalf("chained stdin = %q", got)
	}
}

func TestScriptGTRejectionSkipsChain(t *testing.T) {
	hooksDir, binDir, logDir := scriptFixture(t)
	fakeGT(t, binDir, logDir, 1)
	writeExec(t, filepath.Join(hooksDir, ChainedHookName), "#!/bin/sh\ntouch "+filepath.Join(logDir, "chain.ran")+"\n")
	if _, err := runScript(t, hooksDir, binDir+":"+sysPath, "refs/heads/main 1 refs/heads/main 2\n"); err == nil {
		t.Fatal("gt rejection must reject the push")
	}
	if _, err := os.Stat(filepath.Join(logDir, "chain.ran")); !os.IsNotExist(err) {
		t.Fatal("chained hook must not run after a gt rejection")
	}
}

func TestScriptChainedRejectionRejects(t *testing.T) {
	hooksDir, binDir, logDir := scriptFixture(t)
	fakeGT(t, binDir, logDir, 0)
	writeExec(t, filepath.Join(hooksDir, ChainedHookName), "#!/bin/sh\nexit 3\n")
	if _, err := runScript(t, hooksDir, binDir+":"+sysPath, "refs/heads/main 1 refs/heads/main 2\n"); err == nil {
		t.Fatal("chained hook rejection must reject the push")
	}
}

func TestScriptIgnoresNonExecutableChain(t *testing.T) {
	hooksDir, binDir, logDir := scriptFixture(t)
	fakeGT(t, binDir, logDir, 0)
	if err := os.WriteFile(filepath.Join(hooksDir, ChainedHookName), []byte("#!/bin/sh\nexit 3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := runScript(t, hooksDir, binDir+":"+sysPath, "refs/heads/main 1 refs/heads/main 2\n"); err != nil {
		t.Fatalf("non-executable chained hook is ignored like git does: %v\n%s", err, out)
	}
}
