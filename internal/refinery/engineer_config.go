package refinery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/config"
)

// LoadConfig loads the merge queue configuration of the rig in two layers on
// top of DefaultMergeQueueConfig (gt-pd1c.78 tahap 3):
//
//  1. <rig>/config.json merge_queue: every key the engineer knows, including
//     an explicit gates map.
//  2. <rig>/settings/config.json merge_queue (config.LoadRigSettings), the
//     file `gt rig settings` and the formulas read. It wins for every key it
//     sets: merge_strategy, vcs_provider, require_review,
//     delete_merged_branches and run_tests. Its setup/typecheck/lint/build/test
//     commands become gates (settingsGates) unless run_tests is false or
//     config.json already has an explicit gates map; an explicit map wins and
//     that is logged.
//
// run_tests=false is the explicit opt-out: it disables every gate, the
// generated ones and an explicit gates map alike (logged).
//
// Every call starts from DefaultMergeQueueConfig, so a reload never keeps
// gates a file no longer configures. A missing file contributes nothing; an
// unreadable or invalid file is an error and leaves the previous
// configuration in place (the caller must not merge on a half-read one).
func (e *Engineer) LoadConfig() error {
	cfg := DefaultMergeQueueConfig()
	explicitGates, err := e.loadRigConfigJSON(cfg)
	if err != nil {
		return err
	}
	if err := e.applyRigSettings(cfg, explicitGates); err != nil {
		return err
	}
	if !cfg.RunTests && len(cfg.Gates) > 0 {
		_, _ = fmt.Fprintf(e.output, "[Engineer] run_tests=false: %d gate(s) disabled (%s)\n",
			len(cfg.Gates), strings.Join(orderedGateNames(cfg.Gates), ", "))
		cfg.Gates = nil
	}
	e.config = cfg
	e.prProvider = nil
	// Initialize the PR provider when merge_strategy=pr.
	if e.config.MergeStrategy == "pr" {
		if err := e.initPRProvider(); err != nil {
			return fmt.Errorf("initializing PR provider: %w", err)
		}
	}
	return nil
}

// loadRigConfigJSON applies <rig>/config.json merge_queue to cfg and
// reports whether it carried a non-empty gates map.
func (e *Engineer) loadRigConfigJSON(cfg *MergeQueueConfig) (explicitGates bool, err error) {
	data, err := os.ReadFile(filepath.Join(e.rig.Path, "config.json")) //nolint:gosec // G304: rig path from the town layout
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("reading config: %w", err)
	}
	var rawConfig struct {
		MergeQueue json.RawMessage `json:"merge_queue"`
	}
	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return false, fmt.Errorf("parsing config: %w", err)
	}
	if rawConfig.MergeQueue == nil {
		return false, nil
	}
	var mqRaw mergeQueueConfigRaw
	if err := json.Unmarshal(rawConfig.MergeQueue, &mqRaw); err != nil {
		return false, fmt.Errorf("parsing merge_queue config: %w", err)
	}
	if err := mqRaw.applyTo(cfg); err != nil {
		return false, err
	}
	return len(mqRaw.Gates) > 0, nil
}

// mergeQueueConfigRaw is the JSON form of config.json merge_queue: pointers
// keep unset keys at their defaults, durations are strings.
type mergeQueueConfigRaw struct {
	Enabled              *bool                     `json:"enabled"`
	OnConflict           *string                   `json:"on_conflict"`
	RunTests             *bool                     `json:"run_tests"`
	TestCommand          *string                   `json:"test_command"`
	DeleteMergedBranches *bool                     `json:"delete_merged_branches"`
	RetryFlakyTests      *int                      `json:"retry_flaky_tests"`
	PollInterval         *string                   `json:"poll_interval"`
	MaxConcurrent        *int                      `json:"max_concurrent"`
	StaleClaimTimeout    *string                   `json:"stale_claim_timeout"`
	Gates                map[string]*gateConfigRaw `json:"gates"`
	GatesParallel        *bool                     `json:"gates_parallel"`
	AutoPush             *bool                     `json:"auto_push"`
	MergeStrategy        *string                   `json:"merge_strategy"`
	VCSProvider          *string                   `json:"vcs_provider"`
	RequireReview        *bool                     `json:"require_review"`
}

// gateConfigRaw is the JSON-friendly representation of a gate config
// with timeout as a string duration.
type gateConfigRaw struct {
	Cmd     string `json:"cmd"`
	Timeout string `json:"timeout"`
	Phase   string `json:"phase"`
	Order   int    `json:"order"`
}

// applyTo sets every key present in raw on cfg.
func (raw *mergeQueueConfigRaw) applyTo(cfg *MergeQueueConfig) error {
	setBool(&cfg.Enabled, raw.Enabled)
	setString(&cfg.OnConflict, raw.OnConflict)
	setBool(&cfg.RunTests, raw.RunTests)
	setString(&cfg.TestCommand, raw.TestCommand)
	setBool(&cfg.DeleteMergedBranches, raw.DeleteMergedBranches)
	if raw.RetryFlakyTests != nil {
		cfg.RetryFlakyTests = *raw.RetryFlakyTests
	}
	if raw.MaxConcurrent != nil {
		cfg.MaxConcurrent = *raw.MaxConcurrent
	}
	if raw.PollInterval != nil {
		dur, err := time.ParseDuration(*raw.PollInterval)
		if err != nil {
			return fmt.Errorf("invalid poll_interval %q: %w", *raw.PollInterval, err)
		}
		cfg.PollInterval = dur
	}
	if raw.StaleClaimTimeout != nil {
		dur, err := time.ParseDuration(*raw.StaleClaimTimeout)
		if err != nil {
			return fmt.Errorf("invalid stale_claim_timeout %q: %w", *raw.StaleClaimTimeout, err)
		}
		if dur <= 0 {
			return fmt.Errorf("stale_claim_timeout must be positive, got %v", dur)
		}
		cfg.StaleClaimTimeout = dur
	}
	if raw.Gates != nil {
		gates, err := parseGates(raw.Gates)
		if err != nil {
			return err
		}
		cfg.Gates = gates
	}
	setBool(&cfg.GatesParallel, raw.GatesParallel)
	setBool(&cfg.AutoPush, raw.AutoPush)
	setString(&cfg.MergeStrategy, raw.MergeStrategy)
	setString(&cfg.VCSProvider, raw.VCSProvider)
	if raw.RequireReview != nil {
		v := *raw.RequireReview
		cfg.RequireReview = &v
	}
	return nil
}

func parseGates(raw map[string]*gateConfigRaw) (map[string]*GateConfig, error) {
	gates := make(map[string]*GateConfig, len(raw))
	for name, r := range raw {
		if r == nil {
			return nil, fmt.Errorf("gate %q is empty", name)
		}
		gc := &GateConfig{Cmd: r.Cmd, Order: r.Order}
		if r.Order < 0 {
			return nil, fmt.Errorf("gate %q order must not be negative, got %d", name, r.Order)
		}
		if r.Timeout != "" {
			dur, err := time.ParseDuration(r.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid timeout for gate %q: %w", name, err)
			}
			if dur <= 0 {
				return nil, fmt.Errorf("gate %q timeout must be positive, got %v", name, dur)
			}
			gc.Timeout = dur
		}
		switch r.Phase {
		case "", "pre-merge":
			gc.Phase = GatePhasePreMerge
		case "post-squash":
			gc.Phase = GatePhasePostSquash
		default:
			return nil, fmt.Errorf("gate %q has invalid phase %q: must be \"pre-merge\" or \"post-squash\"", name, r.Phase)
		}
		gates[name] = gc
	}
	return gates, nil
}

// applyRigSettings layers <rig>/settings/config.json merge_queue on cfg
// (settings win). explicitGates: config.json had its own gates map, which
// then wins over the gates generated from the settings commands.
func (e *Engineer) applyRigSettings(cfg *MergeQueueConfig, explicitGates bool) error {
	settings, err := config.LoadRigSettings(config.RigSettingsPath(e.rig.Path))
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("loading %s: %w", config.RigSettingsPath(e.rig.Path), err)
	}
	mq := settings.MergeQueue
	if mq == nil {
		return nil
	}
	setString(&cfg.MergeStrategy, nonEmpty(mq.MergeStrategy))
	setString(&cfg.VCSProvider, nonEmpty(mq.VCSProvider))
	if mq.RequireReview != nil {
		v := *mq.RequireReview
		cfg.RequireReview = &v
	}
	setBool(&cfg.DeleteMergedBranches, mq.DeleteMergedBranches)
	setBool(&cfg.RunTests, mq.RunTests)

	generated := settingsGates(mq)
	switch {
	case len(generated) == 0:
	case !cfg.RunTests:
		// LoadConfig reports and disables every gate.
	case explicitGates:
		_, _ = fmt.Fprintf(e.output, "[Engineer] merge_queue.gates in config.json override the %d gate(s) generated from settings/config.json commands (%s)\n",
			len(generated), strings.Join(orderedGateNames(generated), ", "))
	default:
		cfg.Gates = generated
	}
	return nil
}

// settingsGates turns the settings/config.json commands into gates: setup
// pre-merge, then typecheck, lint, build, test post-squash in that order.
// Blank commands are skipped.
func settingsGates(mq *config.MergeQueueConfig) map[string]*GateConfig {
	specs := []struct {
		name  string
		cmd   string
		phase GatePhase
	}{
		{"setup", mq.SetupCommand, GatePhasePreMerge},
		{"typecheck", mq.TypecheckCommand, GatePhasePostSquash},
		{"lint", mq.LintCommand, GatePhasePostSquash},
		{"build", mq.BuildCommand, GatePhasePostSquash},
		{"test", mq.TestCommand, GatePhasePostSquash},
	}
	gates := make(map[string]*GateConfig)
	for order, spec := range specs {
		if strings.TrimSpace(spec.cmd) == "" {
			continue
		}
		gates[spec.name] = &GateConfig{Cmd: spec.cmd, Phase: spec.phase, Order: order}
	}
	return gates
}

// orderedGateNames returns the gate names sorted by (Order, name).
func orderedGateNames(gates map[string]*GateConfig) []string {
	names := make([]string, 0, len(gates))
	for name := range gates {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		oi, oj := gates[names[i]].Order, gates[names[j]].Order
		if oi != oj {
			return oi < oj
		}
		return names[i] < names[j]
	})
	return names
}

// effectiveGatePhase is the gate's phase, pre-merge when unset.
func effectiveGatePhase(gc *GateConfig) GatePhase {
	if gc == nil || gc.Phase == "" {
		return GatePhasePreMerge
	}
	return gc.Phase
}

// initPRProvider creates the appropriate PRProvider based on vcs_provider config.
// Defaults to GitHub when vcs_provider is empty or "github".
func (e *Engineer) initPRProvider() error {
	switch e.config.VCSProvider {
	case "", "github":
		e.prProvider = newGitHubPRProvider(e.git)
	case "bitbucket":
		p, err := newBitbucketPRProvider(e.git)
		if err != nil {
			return err
		}
		e.prProvider = p
	default:
		return fmt.Errorf("unknown vcs_provider %q (supported: github, bitbucket)", e.config.VCSProvider)
	}
	return nil
}

func setBool(dst *bool, src *bool) {
	if src != nil {
		*dst = *src
	}
}

func setString(dst *string, src *string) {
	if src != nil {
		*dst = *src
	}
}

func nonEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// configuredGateCount is how many gate commands a merge runs: the gates, or
// the legacy test command (run_tests with test_command and no gates).
func (e *Engineer) configuredGateCount() int {
	if len(e.config.Gates) > 0 {
		return len(e.config.Gates)
	}
	if e.config.RunTests && strings.TrimSpace(e.config.TestCommand) != "" {
		return 1
	}
	return 0
}
