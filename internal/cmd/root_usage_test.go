package cmd

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// newUsageTestTree builds a small command tree that mirrors how gt commands
// fail: a runtime error from RunE, plus the cobra-level usage errors (unknown
// flag, wrong argument count, missing required flag) that must keep Usage.
func newUsageTestTree() (*cobra.Command, *bytes.Buffer) {
	out := &bytes.Buffer{}
	root := &cobra.Command{Use: "gt"}
	root.SetOut(out)
	root.SetErr(out)

	run := &cobra.Command{
		Use:  "run",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("not all services started")
		},
	}
	run.Flags().Bool("quiet", false, "quiet")

	required := &cobra.Command{
		Use: "required",
		RunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}
	required.Flags().String("target", "", "target")
	_ = required.MarkFlagRequired("target")

	// A subcommand with its own PersistentPreRunE shadows the root hook in
	// cobra (EnableTraverseRunHooks is off), so the mechanism must not
	// depend on the root's PersistentPreRunE.
	shadow := &cobra.Command{
		Use: "shadow",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return errors.New("runtime failure behind a shadowing hook")
		},
	}

	root.AddCommand(run, required, shadow)
	silenceUsageOnRuntimeErrors(root)
	return root, out
}

func TestRuntimeErrorDoesNotPrintUsage(t *testing.T) {
	for _, args := range [][]string{{"run"}, {"shadow"}} {
		root, out := newUsageTestTree()
		root.SetArgs(args)
		err := root.Execute()
		if err == nil {
			t.Fatalf("%v: expected runtime error", args)
		}
		if strings.Contains(out.String(), "Usage:") {
			t.Errorf("%v: runtime error printed Usage:\n%s", args, out.String())
		}
		if !strings.Contains(out.String(), "Error: ") {
			t.Errorf("%v: runtime error message itself must still be printed:\n%s", args, out.String())
		}
	}
}

func TestUsageErrorsStillPrintUsage(t *testing.T) {
	cases := map[string][]string{
		"unknown flag":          {"run", "--bogus"},
		"bad flag value":        {"run", "--quiet=maybe"},
		"wrong arg count":       {"run", "extra"},
		"missing required flag": {"required"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			root, out := newUsageTestTree()
			root.SetArgs(args)
			if err := root.Execute(); err == nil {
				t.Fatalf("expected usage error for %v", args)
			}
			if !strings.Contains(out.String(), "Usage:") {
				t.Errorf("usage error %v did not print Usage:\n%s", args, out.String())
			}
		})
	}
}

func TestSilenceUsageOnRuntimeErrorsIsIdempotent(t *testing.T) {
	root, out := newUsageTestTree()
	silenceUsageOnRuntimeErrors(root) // second install must not double-wrap or break
	root.SetArgs([]string{"run"})
	if err := root.Execute(); err == nil || err.Error() != "not all services started" {
		t.Fatalf("err = %v, want the original RunE error unchanged", err)
	}
	if strings.Contains(out.String(), "Usage:") {
		t.Errorf("runtime error printed Usage after double install:\n%s", out.String())
	}
}

// The real gt tree: `gt up` and `gt sling` both reported runtime failures with
// a Usage block (gt-pd1c.38). They must be covered by the installer that
// Execute applies to rootCmd.
func TestRootTreeCoversUpAndSling(t *testing.T) {
	silenceUsageOnRuntimeErrors(rootCmd)
	for _, name := range []string{"up", "sling"} {
		cmd, _, err := rootCmd.Find([]string{name})
		if err != nil || cmd == nil || cmd.Name() != name {
			t.Fatalf("find %q: cmd=%v err=%v", name, cmd, err)
		}
		if cmd.RunE == nil {
			t.Fatalf("%q has no RunE", name)
		}
		if cmd.Annotations[silenceUsageAnnotation] != "true" {
			t.Errorf("%q RunE is not wrapped to silence usage on runtime errors", name)
		}
	}
}

// runConvoyStage used to reset SilenceUsage to convoyStageJSON inside RunE,
// which re-enabled Usage for every non-JSON runtime error and undid the
// wrapper. A command must not turn SilenceUsage back off once RunE starts.
func TestConvoyStageRuntimeErrorKeepsUsageSilenced(t *testing.T) {
	silenceUsageOnRuntimeErrors(rootCmd)
	stage, _, err := rootCmd.Find([]string{"convoy", "stage"})
	if err != nil || stage == nil || stage.Name() != "stage" {
		t.Fatalf("find convoy stage: cmd=%v err=%v", stage, err)
	}

	prevJSON, prevSilence := convoyStageJSON, stage.SilenceUsage
	t.Cleanup(func() { convoyStageJSON, stage.SilenceUsage = prevJSON, prevSilence })
	convoyStageJSON = false
	stage.SilenceUsage = false

	// No bead IDs: validateStageArgs fails before any side effect.
	if err := stage.RunE(stage, nil); err == nil {
		t.Fatal("expected a runtime error for missing bead IDs")
	}
	if !stage.SilenceUsage {
		t.Error("convoy stage turned SilenceUsage back off for a non-JSON runtime error")
	}
}
