package refinery

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/util"
)

// ValidateTestCommand validates that a test command is safe to execute.
// TestCommand comes from the rig's operator-controlled config.json, not from
// user input or PR branches. This validation provides defense-in-depth for the
// trusted infrastructure config path.
func ValidateTestCommand(cmd string) error {
	if strings.TrimSpace(cmd) == "" {
		return fmt.Errorf("test command must not be empty")
	}
	return nil
}

// runTests runs the configured test command and returns the result.
func (e *Engineer) runTests(ctx context.Context) ProcessResult {
	if err := ValidateTestCommand(e.config.TestCommand); err != nil {
		return ProcessResult{
			Success: false,
			Error:   fmt.Sprintf("invalid test command: %v", err),
		}
	}

	// Run the test command with retries for flaky tests
	maxRetries := e.config.RetryFlakyTests
	if maxRetries < 1 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if attempt > 1 {
			_, _ = fmt.Fprintf(e.output, "[Engineer] Retrying tests (attempt %d/%d)...\n", attempt, maxRetries)
		}

		// Trust boundary: TestCommand comes from rig's config.json (operator-controlled
		// infrastructure config), not from PR branches or user input. Shell execution
		// is intentional for flexibility (pipes, env vars, etc).
		_, _ = fmt.Fprintf(e.output, "[Engineer] Executing test command: %s\n", e.config.TestCommand)
		cmd := exec.CommandContext(ctx, "sh", "-c", e.config.TestCommand) //nolint:gosec // G204: TestCommand is from trusted rig config
		util.SetDetachedProcessGroup(cmd)
		cmd.Dir = e.workDir
		cmd.Env = gateEnv(os.Environ())
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err == nil {
			return ProcessResult{Success: true}
		}
		lastErr = err

		// Check if context was canceled
		if ctx.Err() != nil {
			return ProcessResult{
				Success: false,
				Error:   "test run canceled",
			}
		}
	}

	return ProcessResult{
		Success:     false,
		TestsFailed: true,
		Error:       fmt.Sprintf("tests failed after %d attempts: %v", maxRetries, lastErr),
	}
}

// runGate executes a single quality gate command and returns the result.
func (e *Engineer) runGate(ctx context.Context, name string, gate *GateConfig) GateResult {
	start := time.Now()

	if strings.TrimSpace(gate.Cmd) == "" {
		return GateResult{
			Name:    name,
			Success: false,
			Error:   "gate command is empty",
			Elapsed: time.Since(start),
		}
	}

	// Apply per-gate timeout if configured
	gateCtx := ctx
	if gate.Timeout > 0 {
		var cancel context.CancelFunc
		gateCtx, cancel = context.WithTimeout(ctx, gate.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(gateCtx, "sh", "-c", gate.Cmd) //nolint:gosec // G204: Gate commands are from trusted rig config
	util.SetDetachedProcessGroup(cmd)
	cmd.Dir = e.workDir
	cmd.Env = gateEnv(os.Environ())
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	elapsed := time.Since(start)

	if err == nil {
		return GateResult{
			Name:    name,
			Success: true,
			Elapsed: elapsed,
		}
	}

	errMsg := fmt.Sprintf("%v", err)
	if gateCtx.Err() == context.DeadlineExceeded {
		errMsg = fmt.Sprintf("timed out after %v", gate.Timeout)
	}
	if stderrStr := strings.TrimSpace(stderr.String()); stderrStr != "" {
		// Cap stderr to avoid huge error messages (on a rune boundary).
		stderrStr = util.TruncateUTF8(stderrStr, 500)
		errMsg = fmt.Sprintf("%s: %s", errMsg, stderrStr)
	}

	return GateResult{
		Name:    name,
		Success: false,
		Error:   errMsg,
		Elapsed: elapsed,
	}
}

// runMergeGates is the single gate entry point for every merge path (single
// MR, merge_strategy=pr, batch stack and bisection). The working tree must hold
// the merged result (target plus the MR or batch), not yet pushed.
//
// With gates configured, the pre-merge group runs first (in parallel when
// gates_parallel is set) and the post-squash group runs after it passes,
// sequentially. Without gates, the legacy run_tests/test_command runs instead.
// A canceled context is reported as a plain failure, never TestsFailed, so a
// refinery shutdown does not make doMerge reject the MR under test. (The batch
// retry/bisect loops still only check Success.)
func (e *Engineer) runMergeGates(ctx context.Context) ProcessResult {
	if len(e.config.Gates) == 0 && (!e.config.RunTests || e.config.TestCommand == "") {
		return ProcessResult{Success: true} // nothing runs, nothing to guard
	}
	before, err := e.git.Rev("HEAD")
	if err != nil {
		return ProcessResult{Success: false, Error: fmt.Sprintf("read HEAD before gates: %v", err)}
	}
	result := e.runConfiguredGates(ctx)
	if !result.Success && ctx.Err() != nil {
		return ProcessResult{Success: false, GatesRan: result.GatesRan, Error: fmt.Sprintf("gate run canceled: %v", ctx.Err())}
	}
	// Gates run branch-controlled code in the merge worktree. Whatever they
	// did, the commit under test must still be HEAD, or nothing is pushed.
	if after, err := e.git.Rev("HEAD"); err != nil || strings.TrimSpace(after) != strings.TrimSpace(before) {
		return ProcessResult{Success: false, TestsFailed: true, GatesRan: result.GatesRan,
			Error: fmt.Sprintf("a gate changed the merge under test: HEAD moved from %s to %s (gates must not commit, amend or reset)",
				shortSHA(strings.TrimSpace(before)), shortSHA(strings.TrimSpace(after)))}
	}
	return result
}

// runConfiguredGates runs the gates (or the legacy test command) and reports
// how many gate commands ran.
func (e *Engineer) runConfiguredGates(ctx context.Context) ProcessResult {
	switch {
	case len(e.config.Gates) > 0:
		result := e.runGatesForPhase(ctx, GatePhasePreMerge)
		if !result.Success {
			return result
		}
		post := e.runGatesForPhase(ctx, GatePhasePostSquash)
		post.GatesRan += result.GatesRan
		return post
	case e.config.RunTests && e.config.TestCommand != "":
		_, _ = fmt.Fprintf(e.output, "[Engineer] Running tests: %s\n", e.config.TestCommand)
		result := e.runTests(ctx)
		if result.Success {
			_, _ = fmt.Fprintln(e.output, "[Engineer] Tests passed")
		}
		result.TestsFailed = !result.Success
		result.GatesRan = 1
		return result
	default:
		return ProcessResult{Success: true}
	}
}

// resetToPreMerge undoes a local merge by resetting the checked-out target to
// the tip recorded before the merge.
func (e *Engineer) resetToPreMerge(preMergeSHA, target string) error {
	if err := e.git.ResetHard(preMergeSHA); err != nil {
		return fmt.Errorf("failed to reset %s to pre-merge %s: %w", target, shortSHA(preMergeSHA), err)
	}
	return nil
}

// runGatesForPhase executes gates matching the given phase, sorted by
// (Order, name). Gates run in parallel if GatesParallel is true (pre-merge
// only); otherwise sequentially in that order.
// Any single gate failure means overall failure.
func (e *Engineer) runGatesForPhase(ctx context.Context, phase GatePhase) ProcessResult {
	// Filter gates for this phase. Empty phase is treated as pre-merge (default).
	gates := make(map[string]*GateConfig)
	for name, gc := range e.config.Gates {
		if effectiveGatePhase(gc) == phase {
			gates[name] = gc
		}
	}
	if len(gates) == 0 {
		return ProcessResult{Success: true}
	}

	names := orderedGateNames(gates)

	parallel := e.config.GatesParallel && phase == GatePhasePreMerge // post-squash always sequential
	_, _ = fmt.Fprintf(e.output, "[Engineer] Running %d %s gate(s) (parallel=%v)\n", len(names), phase, parallel)

	var results []GateResult

	if parallel {
		results = make([]GateResult, len(names))
		var wg sync.WaitGroup
		for i, name := range names {
			// Log from this goroutine only: e.output is not safe for concurrent writes.
			_, _ = fmt.Fprintf(e.output, "[Engineer] Gate %q: starting (%s)\n", name, gates[name].Cmd)
			wg.Add(1)
			go func(idx int, gateName string) {
				defer wg.Done()
				results[idx] = e.runGate(ctx, gateName, gates[gateName])
			}(i, name)
		}
		wg.Wait()
	} else {
		for _, name := range names {
			_, _ = fmt.Fprintf(e.output, "[Engineer] Gate %q: starting (%s)\n", name, gates[name].Cmd)
			result := e.runGate(ctx, name, gates[name])
			results = append(results, result)
			if !result.Success {
				// Sequential mode: stop on first failure
				break
			}
		}
	}

	// Report results
	var failures []string
	for _, r := range results {
		if r.Success {
			_, _ = fmt.Fprintf(e.output, "[Engineer] Gate %q: passed (%v)\n", r.Name, r.Elapsed.Truncate(time.Millisecond))
		} else {
			_, _ = fmt.Fprintf(e.output, "[Engineer] Gate %q: FAILED (%v) - %s\n", r.Name, r.Elapsed.Truncate(time.Millisecond), r.Error)
			failures = append(failures, fmt.Sprintf("%s: %s", r.Name, r.Error))
		}
	}

	if len(failures) > 0 {
		return ProcessResult{
			Success:     false,
			TestsFailed: true,
			GatesRan:    len(results),
			Error:       fmt.Sprintf("quality gates failed: %s", strings.Join(failures, "; ")),
		}
	}

	_, _ = fmt.Fprintln(e.output, "[Engineer] All quality gates passed")
	return ProcessResult{Success: true, GatesRan: len(results)}
}

// gateEnvDropPrefixes and gateEnvDropNames: gates are branch-controlled code,
// so they run without Gas Town agent identity (GT_*, BD_*, BEADS_*), git
// overrides (GIT_*: GIT_DIR, GIT_ASKPASS, GIT_SSH_COMMAND, ...), the SSH agent
// and credentials. PATH, HOME, locale, TMPDIR and the like are kept.
var (
	gateEnvDropPrefixes = []string{"GT_", "BD_", "BEADS_", "GIT_", "GH_", "GITHUB_", "GITLAB_", "BITBUCKET_",
		"AWS_", "AZURE_", "GOOGLE_", "GCP_", "ANTHROPIC_", "OPENAI_", "CLAUDE_", "DOLT_", "NPM_CONFIG__AUTH", "OTEL_"}
	gateEnvDropNames = map[string]bool{"SSH_AUTH_SOCK": true, "SSH_AGENT_PID": true, "SSH_ASKPASS": true,
		"NPM_TOKEN": true, "NODE_AUTH_TOKEN": true, "DOCKER_AUTH_CONFIG": true, "KUBECONFIG": true}
	gateEnvDropMarkers = []string{"TOKEN", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL", "API_KEY", "APIKEY", "PRIVATE_KEY", "ACCESS_KEY"}
)

// gateEnv returns env without the variables a gate must not see
// (gateEnvDropPrefixes, gateEnvDropNames, and any name containing a
// credential marker such as TOKEN, SECRET, PASSWORD, API_KEY).
func gateEnv(env []string) []string {
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if dropGateEnv(strings.ToUpper(name)) {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func dropGateEnv(name string) bool {
	if gateEnvDropNames[name] {
		return true
	}
	for _, p := range gateEnvDropPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	for _, m := range gateEnvDropMarkers {
		if strings.Contains(name, m) {
			return true
		}
	}
	return false
}

// verifyMergeShape checks, right before a push, that tip is exactly the merge
// the engine built: walking first parents from tip, each submitted head in
// heads (in merge order) is the second parent of a two-parent merge commit,
// or was already contained (a no-op merge), and the walk ends at base. A gate
// or anyone else that committed, amended or reset in the worktree breaks it.
func (e *Engineer) verifyMergeShape(tip, base string, heads []string) error {
	current := strings.TrimSpace(tip)
	for i := len(heads) - 1; i >= 0; i-- {
		head := strings.TrimSpace(heads[i])
		// Parents as the commit records them, not through replace refs
		// (gt-pd1c.86); a read failure refuses.
		parents, err := e.git.CommitParents(current)
		if err != nil {
			return fmt.Errorf("read parents of %s: %w", shortSHA(current), err)
		}
		if len(parents) >= 2 && parents[1] == head {
			if len(parents) > 2 {
				return fmt.Errorf("merge %s has more than two parents", shortSHA(current))
			}
			current = parents[0]
			continue
		}
		if contained, err := e.git.IsAncestor(head, current); err != nil || !contained {
			return fmt.Errorf("%s is not the merge of submitted head %s", shortSHA(current), shortSHA(head))
		}
	}
	if current != strings.TrimSpace(base) {
		return fmt.Errorf("merge does not sit on the target tip %s (found %s)", shortSHA(base), shortSHA(current))
	}
	return nil
}

// headAndTree reads HEAD and its tree.
func (e *Engineer) headAndTree() (string, string, error) {
	head, err := e.git.Rev("HEAD")
	if err != nil {
		return "", "", err
	}
	tree, err := e.git.Rev("HEAD^{tree}")
	if err != nil {
		return "", "", err
	}
	return strings.TrimSpace(head), strings.TrimSpace(tree), nil
}

// verifyBuiltMerge checks that HEAD is still exactly the merge the engine
// built (same commit, same tree) and that it has the merge shape: first
// parent the pre-merge target tip, second parent the pinned submitted head
// (verifyMergeShape). Called after the gates and again right before push.
func (e *Engineer) verifyBuiltMerge(built, tree, preMergeSHA, submittedHead string) error {
	head, headTree, err := e.headAndTree()
	if err != nil {
		return fmt.Errorf("verify the merge before push: %w", err)
	}
	if head != built || headTree != tree {
		return fmt.Errorf("the merge changed after it was built: HEAD %s (tree %s), built %s (tree %s); nothing pushed",
			shortSHA(head), shortSHA(headTree), shortSHA(built), shortSHA(tree))
	}
	if err := e.verifyMergeShape(built, preMergeSHA, []string{submittedHead}); err != nil {
		return fmt.Errorf("the built merge has the wrong shape: %w; nothing pushed", err)
	}
	return nil
}

// defaultBranch is the rig's trusted default branch, the one source for
// eligibility, landing proof, the merge slot and branch protection:
// default_branch in <rig>/config.json, else the remote's own HEAD
// (mergeproof.RigDefaultBranch); "" when neither is known.
func (e *Engineer) defaultBranch() string {
	return mergeproof.RigDefaultBranch(e.rig.Path, e.git)
}
