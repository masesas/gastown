package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/refinery"
)

func TestMQMergeCallerAllowed(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		ok   bool
	}{
		{"human shell", nil, true},
		{"refinery", []string{"GT_ROLE", "gastown/refinery"}, true},
		{"refinery via BD_ACTOR", []string{"BD_ACTOR", "gastown/refinery"}, true},
		{"crew", []string{"GT_ROLE", "gastown/crew/joe", "GT_CREW", "joe"}, true},
		{"overseer", []string{"BD_ACTOR", "overseer"}, true},
		{"polecat", []string{"GT_ROLE", "gastown/polecats/nux", "GT_POLECAT", "nux"}, false},
		{"witness", []string{"GT_ROLE", "gastown/witness"}, false},
		{"mayor", []string{"GT_ROLE", "mayor"}, false},
		{"deacon", []string{"GT_ROLE", "deacon"}, false},
		{"refinery claiming to be a polecat too", []string{"GT_ROLE", "gastown/refinery", "GT_POLECAT", "nux"}, false},
		{"crew role with a polecat actor", []string{"GT_ROLE", "gastown/crew/joe", "BD_ACTOR", "gastown/polecats/nux"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := prePushEnv(tc.env...)
			err := mqMergeCallerAllowed(gtCallerFrom(getenv), getenv, "gastown")
			if tc.ok && err != nil {
				t.Fatalf("mqMergeCallerAllowed = %v, want allowed", err)
			}
			if !tc.ok && (err == nil || !strings.Contains(err.Error(), "refinery, crew or a human")) {
				t.Fatalf("mqMergeCallerAllowed = %v, want a refusal naming who may merge", err)
			}
		})
	}
}

type fakeMQMergeEngine struct {
	outcome refinery.MergeOutcome
	out     io.Writer
	gotID   string
}

func (f *fakeMQMergeEngine) SetOutput(w io.Writer) { f.out = w }

func (f *fakeMQMergeEngine) MergeOne(_ context.Context, id string) refinery.MergeOutcome {
	f.gotID = id
	if f.out != nil {
		_, _ = io.WriteString(f.out, "[Engineer] engine log line\n")
	}
	return f.outcome
}

func TestRunMQMergeWith_JSON(t *testing.T) {
	for _, tc := range []struct {
		kind     refinery.MergeOutcomeKind
		wantExit bool
	}{
		{refinery.OutcomeMerged, false},
		{refinery.OutcomeConflict, true},
		{refinery.OutcomeGatesFailed, true},
		{refinery.OutcomeScopeViolation, true},
		{refinery.OutcomeCleanupFailed, true},
		{refinery.OutcomePrecondition, true},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			eng := &fakeMQMergeEngine{outcome: refinery.MergeOutcome{Kind: tc.kind, MRID: "gt-mr", Message: "details"}}
			var stdout, stderr bytes.Buffer
			err := runMQMergeWith(context.Background(), eng, "gastown", "gt-mr", true, &stdout, &stderr)
			if eng.gotID != "gt-mr" {
				t.Fatalf("engine got MR %q", eng.gotID)
			}
			var got refinery.MergeOutcome
			if jerr := json.Unmarshal(stdout.Bytes(), &got); jerr != nil {
				t.Fatalf("stdout is not one JSON outcome (%v):\n%s", jerr, stdout.String())
			}
			if got.Kind != tc.kind || got.MRID != "gt-mr" {
				t.Fatalf("JSON outcome = %+v, want kind %s", got, tc.kind)
			}
			if !strings.Contains(stderr.String(), "engine log line") {
				t.Fatalf("engine log not routed to stderr with --json; stderr=%q", stderr.String())
			}
			code, silent := IsSilentExit(err)
			if tc.wantExit != (err != nil) || (err != nil && (!silent || code != 1)) {
				t.Fatalf("err = %v, want exit 1 exactly when not merged", err)
			}
		})
	}
}

func TestRunMQMergeWith_HumanOutput(t *testing.T) {
	t.Run("merged", func(t *testing.T) {
		cleanup := refinery.BranchCleanup{Branch: "polecat/nux/gt-1", RemoteDeleted: true, LocalDeleted: true}
		eng := &fakeMQMergeEngine{outcome: refinery.MergeOutcome{Kind: refinery.OutcomeMerged, MRID: "gt-mr", Target: "main",
			SourceIssue: "gt-1", MergeCommit: "0123456789abcdef", BranchCleanup: &cleanup}}
		var stdout bytes.Buffer
		if err := runMQMergeWith(context.Background(), eng, "gastown", "gt-mr", false, &stdout, io.Discard); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
		for _, want := range []string{"Merged gt-mr", "main", "01234567", "gt-1", "Merged in gt-mr", "remote and local"} {
			if !strings.Contains(stdout.String(), want) {
				t.Errorf("output missing %q:\n%s", want, stdout.String())
			}
		}
	})
	t.Run("cleanup failed tells to rerun post-merge", func(t *testing.T) {
		eng := &fakeMQMergeEngine{outcome: refinery.MergeOutcome{Kind: refinery.OutcomeCleanupFailed, MRID: "gt-mr", Message: "branch cleanup: boom"}}
		var stdout bytes.Buffer
		err := runMQMergeWith(context.Background(), eng, "gastown", "gt-mr", false, &stdout, io.Discard)
		if code, ok := IsSilentExit(err); !ok || code != 1 {
			t.Fatalf("err = %v, want exit 1", err)
		}
		if !strings.Contains(stdout.String(), "gt mq post-merge gastown gt-mr") {
			t.Fatalf("output does not tell to rerun post-merge:\n%s", stdout.String())
		}
	})
	t.Run("refusal is reported with its kind", func(t *testing.T) {
		eng := &fakeMQMergeEngine{outcome: refinery.MergeOutcome{Kind: refinery.OutcomeScopeViolation, MRID: "gt-mr", Message: "merge scope violation: docs/x.md"}}
		var stdout bytes.Buffer
		_ = runMQMergeWith(context.Background(), eng, "gastown", "gt-mr", false, &stdout, io.Discard)
		if !strings.Contains(stdout.String(), "scope_violation") || !strings.Contains(stdout.String(), "docs/x.md") {
			t.Fatalf("output lacks kind and reason:\n%s", stdout.String())
		}
	})
}

func TestReportMQMergeRefusal_JSON(t *testing.T) {
	var stdout bytes.Buffer
	err := reportMQMergeRefusal(&stdout, "gt-mr", true, errString("refusing gt mq merge from agent role witness"))
	var got refinery.MergeOutcome
	if jerr := json.Unmarshal(stdout.Bytes(), &got); jerr != nil || got.Kind != refinery.OutcomePrecondition || !strings.Contains(got.Message, "witness") {
		t.Fatalf("JSON = %q (%v), want a precondition outcome naming the refusal", stdout.String(), jerr)
	}
	if code, ok := IsSilentExit(err); !ok || code != 1 {
		t.Fatalf("err = %v, want exit 1", err)
	}
	stdout.Reset()
	if err := reportMQMergeRefusal(&stdout, "gt-mr", false, errString("nope")); err == nil || err.Error() != "nope" || stdout.Len() != 0 {
		t.Fatalf("non-JSON refusal = %v (stdout %q), want the plain error", err, stdout.String())
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func TestMQMergeCommandRegistered(t *testing.T) {
	cmd, _, err := mqCmd.Find([]string{"merge"})
	if err != nil || cmd == nil || cmd.Name() != "merge" {
		t.Fatalf("gt mq merge not registered: %v", err)
	}
	if cmd.Flags().Lookup("json") == nil {
		t.Fatal("gt mq merge has no --json flag")
	}
	if err := cmd.Args(cmd, []string{"gastown"}); err == nil {
		t.Fatal("gt mq merge accepted one argument; want <rig> <mr-id>")
	}
}

func TestMQMergeCallerAllowed_RigMustMatch(t *testing.T) {
	for _, tc := range []struct {
		name string
		env  []string
		ok   bool
	}{
		{"refinery of this rig", []string{"GT_ROLE", "gastown/refinery"}, true},
		{"refinery of another rig", []string{"GT_ROLE", "otherrig/refinery"}, false},
		{"rig names compare like the engine (case-insensitive)", []string{"GT_ROLE", "GasTown/refinery", "GT_RIG", "GASTOWN"}, true},
		{"refinery with a mismatching GT_RIG", []string{"GT_ROLE", "gastown/refinery", "GT_RIG", "otherrig"}, false},
		{"crew of another rig", []string{"GT_ROLE", "otherrig/crew/joe", "GT_CREW", "joe"}, false},
		{"crew with GT_RIG only", []string{"GT_CREW", "joe", "GT_RIG", "gastown"}, true},
		{"crew without any rig", []string{"GT_CREW", "joe"}, false},
		{"overseer needs no rig", []string{"BD_ACTOR", "overseer"}, true},
		{"human shell needs no rig", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			getenv := prePushEnv(tc.env...)
			err := mqMergeCallerAllowed(gtCallerFrom(getenv), getenv, "gastown")
			if tc.ok != (err == nil) {
				t.Fatalf("mqMergeCallerAllowed = %v, want allowed=%v", err, tc.ok)
			}
			if err != nil && !strings.Contains(err.Error(), "gastown") {
				t.Fatalf("refusal %q does not name the rig", err)
			}
		})
	}
}
