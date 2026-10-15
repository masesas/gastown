package config

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/steveyegge/gastown/internal/constants"
)

var (
	unsafeEmailTagChars = regexp.MustCompile(`[^a-z0-9._-]`)
	repeatedDashes      = regexp.MustCompile(`-{2,}`)
	emailLocalPattern   = regexp.MustCompile(`^[A-Za-z0-9._%+-]+$`)
	emailDomainPattern  = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
)

// sanitizeEmailTag lowercases the tag, maps characters outside [a-z0-9._-]
// to "-", collapses repeated dashes and trims dashes at both ends.
func sanitizeEmailTag(tag string) string {
	tag = unsafeEmailTagChars.ReplaceAllString(strings.ToLower(tag), "-")
	tag = repeatedDashes.ReplaceAllString(tag, "-")
	return strings.Trim(tag, "-")
}

// AgentPlusEmail builds a plus-address for an agent from the overseer's email:
// "vandik@gmail.com" + "posin/refinery" -> "vandik+posin-refinery@gmail.com".
// An overseer email that already has a +tag has that tag replaced
// ("vandik+x@gmail.com" -> "vandik+nuka@gmail.com") so mail stays in one inbox.
// It returns "" when the overseer email is not a plain local@domain address
// (exactly one "@", dotted domain, no special characters) or the tag sanitizes
// to nothing, so callers fall back to the git config identity.
func AgentPlusEmail(overseerEmail, tag string) string {
	local, domain, ok := strings.Cut(strings.TrimSpace(overseerEmail), "@")
	if !ok || strings.Contains(domain, "@") {
		return ""
	}
	if !emailLocalPattern.MatchString(local) || !emailDomainPattern.MatchString(domain) {
		return ""
	}
	if base, _, hasPlus := strings.Cut(local, "+"); hasPlus {
		local = base
	}
	tag = sanitizeEmailTag(tag)
	if local == "" || tag == "" {
		return ""
	}
	return local + "+" + tag + "@" + domain
}

// GitIdentityName returns the commit author/committer name for an agent role.
// It is the single source used by AgentEnv, handoff and the checkpoint dog.
// role is the simple role ("mayor", "deacon", "boot", "witness", "refinery",
// "polecat", "crew", "dog"); rig and agentName are as in AgentEnvConfig.
// Unknown roles return "".
func GitIdentityName(role, rig, agentName string) string {
	switch role {
	case constants.RoleMayor, constants.RoleDeacon:
		return role
	case "boot":
		return "boot"
	case constants.RoleWitness, constants.RoleRefinery:
		return fmt.Sprintf("%s/%s", rig, role)
	case constants.RolePolecat, constants.RoleCrew:
		return agentName
	case "dog":
		if agentName != "" {
			return agentName
		}
		return "dog"
	}
	return ""
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

const (
	overseerEmailTTL       = 5 * time.Minute
	overseerEmailMissedTTL = 15 * time.Second
)

type overseerEmailEntry struct {
	email   string
	modTime time.Time
	at      time.Time
}

var (
	overseerEmailMu    sync.Mutex
	overseerEmailCache = map[string]overseerEmailEntry{}
)

// ResolveOverseerEmail returns the overseer's email for townRoot: the saved
// mayor/overseer.json first, then the global git config user.email (the
// repo-local config is deliberately ignored). Empty if unknown. Results are
// cached per townRoot, invalidated by overseer.json mtime or a TTL (short for
// misses, so a later fix is picked up quickly).
func ResolveOverseerEmail(townRoot string) string {
	if townRoot == "" {
		return ""
	}
	path := OverseerConfigPath(townRoot)
	var modTime time.Time
	if st, err := os.Stat(path); err == nil {
		modTime = st.ModTime()
	}

	overseerEmailMu.Lock()
	e, hit := overseerEmailCache[townRoot]
	overseerEmailMu.Unlock()
	ttl := overseerEmailTTL
	if e.email == "" {
		ttl = overseerEmailMissedTTL
	}
	if hit && e.modTime.Equal(modTime) && time.Since(e.at) < ttl {
		return e.email
	}

	email := ""
	if cfg, err := LoadOverseerConfig(path); err == nil {
		email = strings.TrimSpace(cfg.Email)
	}
	if email == "" {
		email = globalGitEmail()
	}

	overseerEmailMu.Lock()
	overseerEmailCache[townRoot] = overseerEmailEntry{email: email, modTime: modTime, at: time.Now()}
	overseerEmailMu.Unlock()
	return email
}

// globalGitEmail reads user.email from the global git config only.
func globalGitEmail() string {
	out, err := exec.Command("git", "config", "--global", "--get", "user.email").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
