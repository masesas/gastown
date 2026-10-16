package cmd

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// agentSessionEnvVars are the identity variables Gas Town sets only in agent
// sessions (see config.AgentEnv). Any of them being non-empty means the caller
// is an agent, not a human at a terminal. This is a deliberate subset of
// config.IdentityEnvVars: the git author/committer variables are excluded
// because humans commonly set those themselves.
var agentSessionEnvVars = []string{"GT_ROLE", "BD_ACTOR", "GT_POLECAT", "GT_CREW"}

// agentSessionEnvVar returns the name of the first agent identity variable set
// in the environment, or "" when the caller is a human.
func agentSessionEnvVar(getenv func(string) string) string {
	for _, k := range agentSessionEnvVars {
		if strings.TrimSpace(getenv(k)) != "" {
			return k
		}
	}
	return ""
}

// autoLandSetting is the rig's merge_queue.integration_branch_auto_land value
// as seen by the land gate. loadErr is the first line of the settings read
// error, if any (a missing file is not an error: the default is false).
type autoLandSetting struct {
	enabled bool
	loadErr string
}

// loadAutoLandSetting reads the rig's auto-land setting. A missing or
// unreadable settings file yields enabled=false (the documented default), so
// the land gate fails closed.
func loadAutoLandSetting(rigPath string) autoLandSetting {
	settings, err := config.LoadRigSettings(filepath.Join(rigPath, "settings", "config.json"))
	if err != nil {
		if errors.Is(err, config.ErrNotFound) {
			return autoLandSetting{}
		}
		return autoLandSetting{loadErr: firstLine(err.Error())}
	}
	if settings == nil || settings.MergeQueue == nil {
		return autoLandSetting{}
	}
	return autoLandSetting{enabled: settings.MergeQueue.IsIntegrationBranchAutoLandEnabled()}
}

// integrationAutoLandEnabled reports whether auto-land is on for the rig.
func integrationAutoLandEnabled(rigPath string) bool {
	return loadAutoLandSetting(rigPath).enabled
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// checkIntegrationLandAllowed is the single gate for merging an integration
// branch into its target branch (gt-pd1c.71). When the rig has auto-land
// disabled, only a human may land: an agent caller is refused. Dry runs are
// always allowed because they change nothing. With auto-land enabled the
// behaviour is unchanged. --force does not bypass this gate.
//
// The error names the identifying variable but never its value, and tells the
// operator how to run the land from a clean shell.
func checkIntegrationLandAllowed(rigName, epicID string, autoLand autoLandSetting, dryRun bool, getenv func(string) string) error {
	if autoLand.enabled || dryRun {
		return nil
	}
	envVar := agentSessionEnvVar(getenv)
	if envVar == "" {
		return nil
	}
	reason := "integration_branch_auto_land=false"
	if autoLand.loadErr != "" {
		reason = "rig settings unreadable: " + autoLand.loadErr + "; treating auto-land as off"
	}
	return fmt.Errorf("auto-land disabled for rig %s; a human must run gt mq integration land %s "+
		"(caller identified as an agent via %s; %s). "+
		"Operator: run it from a fresh terminal, or: env -u %s gt mq integration land %s "+
		"(%s exported in your shell rc also counts as an agent)",
		rigName, epicID, envVar, reason,
		strings.Join(agentSessionEnvVars, " -u "), epicID, envVar)
}
