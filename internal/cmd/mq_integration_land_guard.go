package cmd

import (
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

// integrationAutoLandEnabled reports the rig's
// merge_queue.integration_branch_auto_land setting. A missing or unreadable
// settings file yields false (the documented default), so the land guard fails
// closed.
func integrationAutoLandEnabled(rigPath string) bool {
	settings, err := config.LoadRigSettings(filepath.Join(rigPath, "settings", "config.json"))
	if err != nil || settings == nil || settings.MergeQueue == nil {
		return false
	}
	return settings.MergeQueue.IsIntegrationBranchAutoLandEnabled()
}

// checkIntegrationLandAllowed is the single gate for merging an integration
// branch into its target branch (gt-pd1c.71). When the rig has auto-land
// disabled, only a human may land: an agent caller is refused. Dry runs are
// always allowed because they change nothing. With auto-land enabled the
// behaviour is unchanged. --force does not bypass this gate.
//
// The error names the identifying variable but never its value.
func checkIntegrationLandAllowed(rigName, epicID string, autoLand, dryRun bool, getenv func(string) string) error {
	if autoLand || dryRun {
		return nil
	}
	envVar := agentSessionEnvVar(getenv)
	if envVar == "" {
		return nil
	}
	return fmt.Errorf("auto-land disabled for rig %s; a human must run gt mq integration land %s "+
		"(caller identified as an agent via %s; integration_branch_auto_land=false)",
		rigName, epicID, envVar)
}
