package config

import (
	"regexp"
	"strings"
)

var unsafeEmailTagChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// AgentPlusEmail builds a plus-address for an agent from the overseer's email:
// "vandik@gmail.com" + "posin/refinery" -> "vandik+posin-refinery@gmail.com".
// Characters outside [A-Za-z0-9._-] in the tag become "-". It returns "" when
// the overseer email is empty or not a plain local@domain address, or the tag
// sanitizes to nothing, so callers fall back to the git config identity.
func AgentPlusEmail(overseerEmail, tag string) string {
	overseerEmail = strings.TrimSpace(overseerEmail)
	at := strings.LastIndex(overseerEmail, "@")
	if at <= 0 || at == len(overseerEmail)-1 || strings.ContainsAny(overseerEmail, " \t\r\n<>,") {
		return ""
	}
	tag = strings.Trim(unsafeEmailTagChars.ReplaceAllString(tag, "-"), "-")
	if tag == "" {
		return ""
	}
	return overseerEmail[:at] + "+" + tag + overseerEmail[at:]
}

// GitIdentityEnv returns the git identity env for an agent: author and
// committer share the agent name and the plus-address email. The email vars
// are omitted when no valid overseer email is available.
func GitIdentityEnv(name, overseerEmail string) map[string]string {
	env := map[string]string{
		"GIT_AUTHOR_NAME":    name,
		"GIT_COMMITTER_NAME": name,
	}
	if email := AgentPlusEmail(overseerEmail, name); email != "" {
		env["GIT_AUTHOR_EMAIL"] = email
		env["GIT_COMMITTER_EMAIL"] = email
	}
	return env
}

// ResolveOverseerEmail returns the overseer's email for townRoot: the saved
// mayor/overseer.json first, then git config user.email. Empty if unknown.
func ResolveOverseerEmail(townRoot string) string {
	if townRoot == "" {
		return ""
	}
	if cfg, err := LoadOverseerConfig(OverseerConfigPath(townRoot)); err == nil && cfg.Email != "" {
		return cfg.Email
	}
	if cfg := detectFromGitConfig(townRoot); cfg != nil {
		return cfg.Email
	}
	return ""
}
