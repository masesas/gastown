package refinery

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	beadsdk "github.com/steveyegge/beads"
	"github.com/steveyegge/gastown/internal/beads"
)

// TestRefineryPath_ClosesOnlyWithMergedIn is the tahap 3 acceptance test
// (gt-pd1c.78): on the Go refinery path every source bead is closed with
// "Merged in <mr>" (so beads.IsBlockerResolved unblocks merge-blocks
// dependents) and every MR bead with the merged reason.
//
//   - runtime: a MergeOne and a two-MR ProcessBatch run against an in-memory
//     store that records every close the engine makes;
//   - static: the refinery package closes beads in exactly the allowed
//     places, and closeMergedWorkBead validates its reason before closing.
func TestRefineryPath_ClosesOnlyWithMergedIn(t *testing.T) {
	t.Run("MergeOne", func(t *testing.T) {
		f := newMergeOneFixture(t)
		if out := f.mergeOne(t); out.Kind != OutcomeMerged {
			t.Fatalf("MergeOne = %s %q, want merged", out.Kind, out.Message)
		}
		assertMergedCloses(t, f.store, map[string]string{fixtureSource: fixtureMR})
	})

	t.Run("ProcessBatch", func(t *testing.T) {
		f := newMergeOneFixture(t)
		second := "polecat/rust/gt-src2"
		createFeatureBranch(t, f.workDir, second, "second.txt", "second\n")
		secondCommit := run(t, f.workDir, "git", "rev-parse", second)
		run(t, f.workDir, "git", "push", "origin", second)
		run(t, f.workDir, "git", "fetch", "origin")
		run(t, f.workDir, "git", "branch", f.branch, "origin/"+f.branch)
		f.store.issues["gt-src2"] = memIssue("gt-src2", "Second feature")
		f.store.issues["gt-mr2"] = memMRIssue("gt-mr2", second, "main", "gt-src2", secondCommit)

		e := f.engineer(t)
		var batch []*MRInfo
		for _, id := range []string{fixtureMR, "gt-mr2"} {
			mr, err := e.LoadMRForMerge(id)
			if err != nil {
				t.Fatalf("LoadMRForMerge(%s): %v", id, err)
			}
			batch = append(batch, mr)
		}
		res := e.ProcessBatch(context.Background(), batch, "main", DefaultBatchConfig())
		if res.Error != nil || len(res.Merged) != 2 {
			t.Fatalf("ProcessBatch: merged=%v err=%v\n%s", mrIDs(res.Merged), res.Error, f.out.String())
		}
		assertMergedCloses(t, f.store, map[string]string{fixtureSource: fixtureMR, "gt-src2": "gt-mr2"})
	})

	t.Run("static close sites", func(t *testing.T) {
		assertRefineryCloseSites(t)
	})
}

// assertMergedCloses checks every recorded close: MR beads (gt:merge-request)
// with exactly the merged reason, every other bead with "Merged in <its MR>",
// and that each source in sourceToMR was closed.
func assertMergedCloses(t *testing.T, store *memStore, sourceToMR map[string]string) {
	t.Helper()
	closed := map[string]bool{}
	for _, c := range store.closeCalls() {
		closed[c.ID] = true
		is := store.issue(t, c.ID)
		if hasAllLabels(is.Labels, []string{"gt:merge-request"}) {
			if c.Reason != string(CloseReasonMerged) {
				t.Errorf("MR %s closed with %q, want %q", c.ID, c.Reason, CloseReasonMerged)
			}
			continue
		}
		mrID, ok := sourceToMR[c.ID]
		if !ok {
			t.Errorf("unexpected close of %s with %q on the refinery path", c.ID, c.Reason)
			continue
		}
		if err := validateMergedCloseReason(c.Reason, mrID); err != nil {
			t.Errorf("source %s: %v", c.ID, err)
		}
		if !beads.IsBlockerResolved("merge-blocks", string(beadsdk.StatusClosed), c.Reason) {
			t.Errorf("source %s close reason %q leaves merge-blocks dependents blocked", c.ID, c.Reason)
		}
	}
	for src := range sourceToMR {
		if !closed[src] {
			t.Errorf("source %s was not closed", src)
		}
	}
}

// refineryCloseSites lists, per close method, the functions of the refinery
// package allowed to call it. Adding a close anywhere else must be reviewed
// against the merged-reason rule and added here deliberately.
var refineryCloseSites = map[string][]string{
	// The one source-bead close: reason validated as "Merged in <mr>".
	"ForceCloseWithReason": {"closeMergedWorkBead"},
	// MR beads (terminal reason chosen by the caller: merged, rejected:, ...)
	// and moot conflict-resolution tasks.
	"CloseWithReason": {"closeConflictTaskIfOpen", "closeTerminalMR"},
	// bd close argv: completed convoys only.
	`"close"`: {"checkAndCloseCompletedConvoys"},
}

func assertRefineryCloseSites(t *testing.T) {
	t.Helper()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	found := map[string]map[string]bool{}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			inspectCloseSites(t, fset, fn, found)
		}
	}
	for site, allowed := range refineryCloseSites {
		var got []string
		for fn := range found[site] {
			got = append(got, fn)
		}
		sort.Strings(got)
		if strings.Join(got, ",") != strings.Join(allowed, ",") {
			t.Errorf("%s is called from %v, want exactly %v", site, got, allowed)
		}
	}
}

func inspectCloseSites(t *testing.T, fset *token.FileSet, fn *ast.FuncDecl, found map[string]map[string]bool) {
	t.Helper()
	record := func(site string) {
		if found[site] == nil {
			found[site] = map[string]bool{}
		}
		found[site][fn.Name.Name] = true
	}
	var validatePos, closePos token.Pos
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CallExpr:
			sel, ok := x.Fun.(*ast.SelectorExpr)
			if ident, isIdent := x.Fun.(*ast.Ident); isIdent && ident.Name == "validateMergedCloseReason" {
				validatePos = x.Pos()
			}
			if !ok {
				return true
			}
			switch sel.Sel.Name {
			case "ForceCloseWithReason", "CloseWithReason":
				record(sel.Sel.Name)
				if sel.Sel.Name == "ForceCloseWithReason" {
					closePos = x.Pos()
				}
			case "Close":
				// MergeRequest.Close(CloseReason...) is an in-memory state
				// transition; any other Close on the refinery path is a bead
				// close that bypasses the rule.
				if len(x.Args) != 1 || !strings.HasPrefix(exprString(x.Args[0]), "CloseReason") {
					t.Errorf("%s: %s calls %s.Close(...) outside the allowed close sites", fset.Position(x.Pos()), fn.Name.Name, exprString(sel.X))
				}
			}
		case *ast.BasicLit:
			if x.Kind == token.STRING {
				if v, err := strconv.Unquote(x.Value); err == nil && v == "close" {
					record(`"close"`)
				}
			}
		}
		return true
	})
	if fn.Name.Name == "closeMergedWorkBead" && (validatePos == token.NoPos || closePos == token.NoPos || validatePos > closePos) {
		t.Errorf("closeMergedWorkBead must call validateMergedCloseReason before ForceCloseWithReason")
	}
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	default:
		return ""
	}
}
