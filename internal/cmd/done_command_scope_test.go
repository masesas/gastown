package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// gt-pd1c.60: the polecat ownership guard must apply to top-level `gt done`
// only. Subcommands that happen to be named "done" (gt dog done, gt mol step
// done, gt wl done) belong to other roles and must not be rejected by it.

func TestIsDoneCommandScopesToTopLevelDoneSynthetic(t *testing.T) {
	root := &cobra.Command{Use: "gt"}
	topDone := &cobra.Command{Use: "done"}
	dog := &cobra.Command{Use: "dog"}
	dogDone := &cobra.Command{Use: "done [name]"}
	mol := &cobra.Command{Use: "mol"}
	step := &cobra.Command{Use: "step"}
	stepDone := &cobra.Command{Use: "done <step-id>"}
	root.AddCommand(topDone, dog, mol)
	dog.AddCommand(dogDone)
	mol.AddCommand(step)
	step.AddCommand(stepDone)
	standalone := &cobra.Command{Use: "done"}

	for _, tt := range []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"gt done", topDone, true},
		{"standalone done", standalone, true},
		{"gt dog done", dogDone, false},
		{"gt mol step done", stepDone, false},
		{"gt dog", dog, false},
		{"root", root, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := isDoneCommand(tt.cmd); got != tt.want {
				t.Fatalf("isDoneCommand(%s) = %v, want %v", tt.cmd.CommandPath(), got, tt.want)
			}
		})
	}
}

func TestIsDoneCommandAndInvocationScopeOnRealTree(t *testing.T) {
	// isDoneCommand does not walk ancestors; that is only sound while
	// top-level gt done has no subcommands.
	if doneCmd.HasSubCommands() {
		t.Fatalf("doneCmd has subcommands; isDoneCommand must be revisited")
	}
	for _, tt := range []struct {
		args []string
		want bool
	}{
		{[]string{"done"}, true},
		{[]string{"done", "--status", "COMPLETED"}, true},
		{[]string{"dog", "done"}, false},
		{[]string{"dog", "done", "alpha"}, false},
		{[]string{"mol", "step", "done", "gt-abc.1"}, false},
		{[]string{"wl", "done", "w-1"}, false},
		{[]string{}, false},
	} {
		t.Run(strings.Join(append([]string{"gt"}, tt.args...), " "), func(t *testing.T) {
			cmd, _, err := rootCmd.Find(tt.args)
			if err != nil {
				t.Fatalf("rootCmd.Find(%v): %v", tt.args, err)
			}
			if len(tt.args) > 0 && cmd.Name() != "done" {
				t.Fatalf("rootCmd.Find(%v) resolved %q, want a command named done", tt.args, cmd.CommandPath())
			}
			if got := isDoneCommand(cmd); got != tt.want {
				t.Fatalf("isDoneCommand(%s) = %v, want %v", cmd.CommandPath(), got, tt.want)
			}
			if got := isDoneInvocation(tt.args); got != tt.want {
				t.Fatalf("isDoneInvocation(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
	}
}

func TestPersistentPreRunDogDoneSkipsPolecatGuard(t *testing.T) {
	// A real (temp) town so the guard reaches its actor check, reproducing
	// the field error "gt done is for polecats only (BD_ACTOR=dog)".
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "mayor"), 0755); err != nil {
		t.Fatalf("mkdir mayor: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mayor", "town.json"), []byte("{}"), 0644); err != nil {
		t.Fatalf("write town.json: %v", err)
	}
	t.Setenv("GT_TOWN_ROOT", dir)
	t.Setenv("GT_ROOT", dir)
	t.Setenv("BD_ACTOR", "dog")
	t.Setenv("GT_ROLE", "dog")
	t.Setenv("GT_RIG", "")
	t.Setenv("GT_POLECAT", "")
	t.Setenv("GT_STALE_WARNED", "1")
	oldUsage := logUsagePath
	logUsagePath = filepath.Join(dir, "cmd-usage.jsonl")
	t.Cleanup(func() { logUsagePath = oldUsage })

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })
	markBuiltProperlyForTest(t)

	dogDone, _, err := rootCmd.Find([]string{"dog", "done"})
	if err != nil {
		t.Fatalf("rootCmd.Find(dog done): %v", err)
	}
	if err := persistentPreRun(dogDone, nil); err != nil {
		t.Fatalf("persistentPreRun(gt dog done) = %v, want no polecat guard error", err)
	}

	// Fail-closed control: the same environment still rejects top-level gt done.
	done, _, err := rootCmd.Find([]string{"done"})
	if err != nil {
		t.Fatalf("rootCmd.Find(done): %v", err)
	}
	err = persistentPreRun(done, nil)
	if err == nil || !strings.Contains(err.Error(), "polecats only") {
		t.Fatalf("persistentPreRun(gt done) = %v, want polecat guard rejection", err)
	}
}
