package polecat

import (
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/mergeproof"
)

func TestFormatGeneratedBranchName_ActionCompatible(t *testing.T) {
	branch := FormatGeneratedBranchName("alpha", "gt-pin-bd-metadata", "mk123456")
	if strings.Contains(branch, "@") {
		t.Fatalf("FormatGeneratedBranchName() = %q, must not contain @", branch)
	}

	claudeCodeActionBranch := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9/_.#+,-]*$`)
	if !claudeCodeActionBranch.MatchString(branch) {
		t.Fatalf("FormatGeneratedBranchName() = %q, rejected by claude-code-action branch pattern", branch)
	}
	if err := exec.Command("git", "check-ref-format", "--branch", branch).Run(); err != nil {
		t.Fatalf("FormatGeneratedBranchName() = %q, rejected by git check-ref-format: %v", branch, err)
	}
}

func TestParseBranchName(t *testing.T) {
	tests := []struct {
		name          string
		branch        string
		wantOk        bool
		wantGenerated bool
		wantPolecat   string
		wantIssue     string
	}{
		{
			name:          "generated issue with plus suffix",
			branch:        "polecat/alpha/gt-pin-bd-metadata+mk123456",
			wantOk:        true,
			wantGenerated: true,
			wantPolecat:   "alpha",
			wantIssue:     "gt-pin-bd-metadata",
		},
		{
			name:          "generated dotted subtask with plus suffix",
			branch:        "polecat/alpha/gt-4kp9.5.5.1+mk123456",
			wantOk:        true,
			wantGenerated: true,
			wantPolecat:   "alpha",
			wantIssue:     "gt-4kp9.5.5.1",
		},
		{
			name:          "legacy generated issue with at suffix",
			branch:        "polecat/alpha/gt-jns7.1@mk123456",
			wantOk:        true,
			wantGenerated: true,
			wantPolecat:   "alpha",
			wantIssue:     "gt-jns7.1",
		},
		{
			name:        "raw dashed issue slug is not truncated",
			branch:      "polecat/alpha/gt-pin-bd-metadata",
			wantOk:      true,
			wantPolecat: "alpha",
			wantIssue:   "gt-pin-bd-metadata",
		},
		{
			name:          "no issue generated branch",
			branch:        "polecat/alpha-mk123456",
			wantOk:        true,
			wantGenerated: true,
			wantPolecat:   "alpha",
		},
		{
			name:   "empty generated suffix is invalid",
			branch: "polecat/alpha/gt-abc+",
		},
		{
			name:   "empty generated issue is invalid",
			branch: "polecat/alpha/+mk123456",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseBranchName(tt.branch)
			if ok != tt.wantOk {
				t.Fatalf("ParseBranchName(%q) ok = %v, want %v", tt.branch, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if got.Generated != tt.wantGenerated {
				t.Errorf("Generated = %v, want %v", got.Generated, tt.wantGenerated)
			}
			if got.Polecat != tt.wantPolecat {
				t.Errorf("Polecat = %q, want %q", got.Polecat, tt.wantPolecat)
			}
			if got.Issue != tt.wantIssue {
				t.Errorf("Issue = %q, want %q", got.Issue, tt.wantIssue)
			}
		})
	}
}

func TestParseGeneratedBranchNameRejectsRawDashedIssues(t *testing.T) {
	rejects := []string{
		"polecat/alpha/gt-pin-bd-metadata",
		"polecat/alpha/gt-jns7.1-mk123456",
	}
	for _, branch := range rejects {
		if meta, ok := ParseGeneratedBranchName(branch); ok {
			t.Errorf("ParseGeneratedBranchName(%q) = %+v, want ok=false", branch, meta)
		}
	}
}

// gt-pd1c.78 (e): one ownership rule; a flat dashed branch names both
// candidates.
func TestBranchOwners(t *testing.T) {
	cases := map[string][]string{
		"polecat/nux":           {"nux"},
		"polecat/nux/gt-1+abc":  {"nux"},
		"polecat/nux/gt-1":      {"nux"},
		"polecat/nux-abc123":    {"nux-abc123", "nux"},
		"polecat/nux-2-abc":     {"nux-2-abc", "nux-2"},
		"polecat/nux-2":         {"nux-2", "nux"},
		"polecat/":              nil,
		"main":                  nil,
		"integration/polecat/x": nil,
	}
	for branch, want := range cases {
		got := BranchOwners(branch)
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("BranchOwners(%q) = %v, want %v", branch, got, want)
		}
	}
}

// Every branch the generator emits (with and without an issue, with real
// base36 millisecond suffixes) is a branch name the coordinator accepts: the
// one ref-name rule (beads.IsSafeRefName), the abandon archive ref
// (mergeproof.ArchiveRef round trip) and BranchOwners. The legacy '@' form is
// still accepted for in-flight branches.
func TestGeneratedBranchNamesPassTheRefNameRule(t *testing.T) {
	tip := strings.Repeat("ab", 20)
	at := time.UnixMilli(1760000000123)
	suffix := strconv.FormatInt(at.UnixMilli(), 36)
	branches := []string{
		FormatGeneratedBranchName("mirelurk", "kp-znd", suffix),
		FormatGeneratedBranchName("s2test", "kp-s2b", "t1"),
		FormatGeneratedBranchName("Nux", "gt-abc.12", suffix),
		FormatGeneratedBranchName("nux", "", suffix),
		"polecat/nux/gt-1@" + suffix,
	}
	for _, b := range branches {
		if !beads.IsSafeRefName(b) {
			t.Errorf("IsSafeRefName(%q) = false", b)
		}
		ref, err := mergeproof.ArchiveRef(b, at, tip)
		if err != nil {
			t.Errorf("ArchiveRef(%q): %v", b, err)
			continue
		}
		if got, _, ok := mergeproof.ParseArchiveRef(ref); !ok || got != b {
			t.Errorf("ParseArchiveRef(%q) = %q, %v; want %q", ref, got, ok, b)
		}
		meta, ok := ParseBranchName(b)
		if !ok || !containsOwner(BranchOwners(b), meta.Polecat) {
			t.Errorf("BranchOwners(%q) = %v, ParseBranchName = %+v %v", b, BranchOwners(b), meta, ok)
		}
	}
}

func containsOwner(owners []string, name string) bool {
	for _, o := range owners {
		if o == name {
			return true
		}
	}
	return false
}
