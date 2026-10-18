package cmd

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/steveyegge/gastown/internal/refinery"
)

// fakeMQPostMergeEngine records the PostMerge call of gt mq post-merge, which
// is a thin wrapper over the engine's one finalize (refinery.Engineer.PostMerge).
type fakeMQPostMergeEngine struct {
	mr     *refinery.MRInfo
	report refinery.FinalizeReport
	err    error

	calledWith string
	opts       refinery.PostMergeOptions
	output     io.Writer
}

func (f *fakeMQPostMergeEngine) PostMerge(mrID string, opts refinery.PostMergeOptions) (*refinery.MRInfo, refinery.FinalizeReport, error) {
	f.calledWith, f.opts = mrID, opts
	return f.mr, f.report, f.err
}

func (f *fakeMQPostMergeEngine) SetOutput(w io.Writer) { f.output = w }

func postMergeMRInfo() *refinery.MRInfo {
	return &refinery.MRInfo{ID: "gt-mr", Branch: "polecat/nux/gt-1+mvcf8o91", Target: "main", SourceIssue: "gt-1", CommitSHA: "abc123def456"}
}

func TestRunMQPostMergeWith_FinishedFinalize(t *testing.T) {
	eng := &fakeMQPostMergeEngine{mr: postMergeMRInfo(), report: refinery.FinalizeReport{
		BranchCleanup: refinery.BranchCleanup{Branch: "polecat/nux/gt-1+mvcf8o91", RemoteDeleted: true, LocalDeleted: true},
	}}
	var out bytes.Buffer
	if err := runMQPostMergeWith(eng, "gt-mr", refinery.PostMergeOptions{SkipBranchDelete: true}, &out); err != nil {
		t.Fatalf("runMQPostMergeWith = %v, want nil\n%s", err, out.String())
	}
	if eng.calledWith != "gt-mr" || !eng.opts.SkipBranchDelete || eng.output != &out {
		t.Fatalf("engine called with %q %+v output=%v", eng.calledWith, eng.opts, eng.output)
	}
	for _, want := range []string{"gt-mr", "Merged in gt-mr", "deleted (remote and local)"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("output %q does not contain %q", out.String(), want)
		}
	}
}

func TestRunMQPostMergeWith_RefusalAndFailedStepsExitNonZero(t *testing.T) {
	refused := &fakeMQPostMergeEngine{err: errors.New("MR gt-mr: the landing is not bound to this MR: the MR has no recorded merge (merge_commit) of it")}
	var out bytes.Buffer
	err := runMQPostMergeWith(refused, "gt-mr", refinery.PostMergeOptions{}, &out)
	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("refusal: err = %v, want the engine's refusal", err)
	}

	unbound := &fakeMQPostMergeEngine{mr: postMergeMRInfo(), report: refinery.FinalizeReport{
		SourceErr: errors.New("source gt-1 is not bound to MR gt-mr"),
	}}
	out.Reset()
	err = runMQPostMergeWith(unbound, "gt-mr", refinery.PostMergeOptions{}, &out)
	if err == nil || !strings.Contains(err.Error(), "not bound") {
		t.Fatalf("unbound source: err = %v, want a failure naming the source", err)
	}
	if strings.Contains(out.String(), "Merged in gt-mr") {
		t.Fatalf("output claims the source closed: %q", out.String())
	}
}

func TestMQPostMergeCallerAllowed(t *testing.T) {
	for name, tc := range map[string]struct {
		env []string
		ok  bool
	}{
		"human":               {nil, true},
		"refinery of the rig": {[]string{"GT_ROLE", "gastown/refinery"}, true},
		"crew of the rig":     {[]string{"GT_ROLE", "gastown/crew/max"}, true},
		"polecat":             {[]string{"GT_ROLE", "gastown/polecats/nux"}, false},
		"witness":             {[]string{"GT_ROLE", "gastown/witness"}, false},
		"refinery of another": {[]string{"GT_ROLE", "other/refinery"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			getenv := prePushEnv(tc.env...)
			err := mqRefineryCallerAllowed("gt mq post-merge", gtCallerFrom(getenv), getenv, "gastown")
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v, want allowed=%v", err, tc.ok)
			}
			if err != nil && !strings.Contains(err.Error(), "gt mq post-merge") {
				t.Fatalf("refusal %q does not name the command", err)
			}
		})
	}
}
