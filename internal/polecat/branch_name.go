package polecat

import (
	"fmt"
	"strings"
)

const (
	polecatBranchPrefix           = "polecat/"
	generatedIssueBranchSeparator = "+"
	legacyIssueBranchSeparator    = "@"
)

// BranchNameMeta is the structured identity encoded in a polecat branch name.
type BranchNameMeta struct {
	Polecat   string
	Issue     string
	Generated bool
}

// FormatGeneratedBranchName returns the canonical generated polecat branch.
func FormatGeneratedBranchName(polecatName, issue, suffix string) string {
	if issue != "" {
		return fmt.Sprintf("%s%s/%s%s%s", polecatBranchPrefix, polecatName, issue, generatedIssueBranchSeparator, suffix)
	}
	return fmt.Sprintf("%s%s-%s", polecatBranchPrefix, polecatName, suffix)
}

// BranchOwners returns every polecat name that may own branch, the one
// ownership rule (gt-pd1c.78): polecat/<name>, polecat/<name>/..., and the
// name ParseBranchName decodes (polecat/<name>-<suffix>). A flat dashed
// branch is ambiguous (polecat/nux-2 may be nux-2's branch or nux's with
// suffix 2), so both names are returned; callers that need one owner treat
// any of them as the owner. Not a polecat branch: nil.
func BranchOwners(branch string) []string {
	rest, ok := strings.CutPrefix(branch, polecatBranchPrefix)
	if !ok || rest == "" {
		return nil
	}
	var out []string
	if slash := strings.Index(rest, "/"); slash > 0 {
		out = append(out, rest[:slash])
	} else if slash < 0 {
		out = append(out, rest)
	}
	if meta, ok := ParseBranchName(branch); ok && meta.Polecat != "" && (len(out) == 0 || out[0] != meta.Polecat) {
		out = append(out, meta.Polecat)
	}
	return out
}

// ParseBranchName decodes polecat branch names without guessing at dashed issue IDs.
func ParseBranchName(branch string) (BranchNameMeta, bool) {
	if !strings.HasPrefix(branch, polecatBranchPrefix) {
		return BranchNameMeta{}, false
	}

	rest := branch[len(polecatBranchPrefix):]
	if rest == "" {
		return BranchNameMeta{}, false
	}

	if slash := strings.Index(rest, "/"); slash >= 0 {
		if slash == 0 {
			return BranchNameMeta{}, false
		}
		polecatName := rest[:slash]
		issueTail := rest[slash+1:]
		if issueTail == "" || strings.Contains(issueTail, "/") {
			return BranchNameMeta{}, false
		}
		issue, generated, ok := parseIssueTail(issueTail)
		if !ok {
			return BranchNameMeta{}, false
		}
		return BranchNameMeta{Polecat: polecatName, Issue: issue, Generated: generated}, true
	}

	dash := strings.LastIndex(rest, "-")
	if dash <= 0 || dash == len(rest)-1 {
		return BranchNameMeta{}, false
	}
	return BranchNameMeta{Polecat: rest[:dash], Generated: true}, true
}

// ParseGeneratedBranchName decodes only branch names emitted by FormatGeneratedBranchName
// and the legacy @ issue-suffix form kept for in-flight branches.
func ParseGeneratedBranchName(branch string) (BranchNameMeta, bool) {
	meta, ok := ParseBranchName(branch)
	if !ok || !meta.Generated {
		return BranchNameMeta{}, false
	}
	return meta, true
}

func parseIssueTail(issueTail string) (issue string, generated bool, ok bool) {
	delim := -1
	for _, sep := range []string{generatedIssueBranchSeparator, legacyIssueBranchSeparator} {
		if idx := strings.Index(issueTail, sep); idx >= 0 && (delim == -1 || idx < delim) {
			delim = idx
		}
	}
	if delim >= 0 {
		if delim == 0 || delim == len(issueTail)-1 {
			return "", false, false
		}
		return issueTail[:delim], true, true
	}
	return issueTail, false, true
}
