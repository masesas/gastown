package cmd

import (
	"strings"

	"github.com/steveyegge/gastown/internal/polecat"
)

// gtCaller is who runs a guarded Gas Town action, read from the agent identity
// variables (agentSessionEnvVars). It is the one classification used by the
// coordinator guards (gt-pd1c.78): the pre-push guard, gt polecat nuke --force
// and the manual landing verdict. It is a guard against agents taking a
// human's exit by mistake, not an authentication boundary: anyone can change
// their environment.
type gtCaller struct {
	// role is the parsed role (parseRoleString); "" for a human. A value that
	// is not a known Role (including conflicting agent identities) counts as
	// an agent (fail closed).
	role Role
	// name is the polecat name when every identity variable that names one
	// agrees (GT_ROLE <rig>/polecats/<name>, GT_POLECAT, BD_ACTOR); else "".
	name string
	// via names the first variable that identified an agent; "" for a human.
	via string
	// actor is BD_ACTOR, trimmed (may be empty).
	actor string
	// exemptAll: every identity variable that is set names a human-equivalent
	// caller (crew, or the overseer identity).
	exemptAll bool
}

// roleOverseer is the identity a human overseer uses (BD_ACTOR=overseer, the
// mail identity of a human at a terminal). It is not an agent role.
const roleOverseer Role = "overseer"

// roleConflict marks identity variables naming different agent roles.
const roleConflict Role = "conflicting-identity"

// callerClaim is what one identity variable says about the caller.
type callerClaim struct {
	role   Role
	name   string // polecat name, if the claim names one
	exempt bool   // human-equivalent: crew, or the overseer identity
}

// gtCallerFrom classifies the caller. Conflicts are read the strictest way
// (gt-pd1c.78 round 1, M2): the caller is exempt (human-equivalent) only when
// every identity variable that is set (GT_ROLE, BD_ACTOR, GT_POLECAT,
// GT_CREW) says crew, or BD_ACTOR says overseer while GT_ROLE and GT_POLECAT
// are empty. Otherwise the role is the agent role the non-exempt claims agree
// on, or roleConflict when they disagree.
func gtCallerFrom(getenv func(string) string) gtCaller {
	c := gtCaller{via: agentSessionEnvVar(getenv), actor: strings.TrimSpace(getenv("BD_ACTOR"))}
	if c.via == "" {
		return c
	}
	gtRole := strings.TrimSpace(getenv(EnvGTRole))
	gtPolecat := strings.TrimSpace(getenv("GT_POLECAT"))
	var claims []callerClaim
	if gtRole != "" {
		role, _, name := parseRoleString(gtRole)
		claims = append(claims, callerClaim{role: role, name: polecatNameOf(role, name), exempt: role == RoleCrew})
	}
	if c.actor != "" {
		claims = append(claims, actorClaim(c.actor, gtRole == "" && gtPolecat == ""))
	}
	if gtPolecat != "" {
		claims = append(claims, callerClaim{role: RolePolecat, name: gtPolecat})
	}
	if strings.TrimSpace(getenv("GT_CREW")) != "" {
		claims = append(claims, callerClaim{role: RoleCrew, exempt: true})
	}
	c.role, c.name, c.exemptAll = resolveClaims(claims)
	return c
}

// actorClaim reads BD_ACTOR. "overseer" is human-equivalent only when no
// GT_ROLE or GT_POLECAT is set (overseerAllowed).
func actorClaim(actor string, overseerAllowed bool) callerClaim {
	if isPolecatActor(actor) {
		return callerClaim{role: RolePolecat, name: strings.Split(actor, "/")[2]}
	}
	role, _, name := parseRoleString(actor)
	exempt := role == RoleCrew || (role == roleOverseer && overseerAllowed)
	return callerClaim{role: role, name: polecatNameOf(role, name), exempt: exempt}
}

func polecatNameOf(role Role, name string) string {
	if role == RolePolecat {
		return strings.TrimSpace(name)
	}
	return ""
}

// resolveClaims combines the claims: all exempt -> exempt (crew, or overseer
// when that is the only kind); otherwise the agreed agent role (roleConflict
// when agent claims disagree) and the agreed polecat name.
func resolveClaims(claims []callerClaim) (Role, string, bool) {
	var agent []callerClaim
	exemptRole := RoleCrew
	for _, cl := range claims {
		if cl.exempt {
			if cl.role == roleOverseer {
				exemptRole = roleOverseer
			}
			continue
		}
		agent = append(agent, cl)
	}
	if len(agent) == 0 {
		return exemptRole, "", true
	}
	role, name := agent[0].role, agent[0].name
	for _, cl := range agent[1:] {
		if cl.role != role {
			return roleConflict, "", false
		}
		if cl.name != "" && name != "" && cl.name != name {
			name = "" // polecat names disagree: no owner
			role = roleConflict
		} else if name == "" {
			name = cl.name
		}
	}
	return role, name, false
}

// human reports whether no agent identity is set.
func (c gtCaller) human() bool { return c.via == "" }

// exempt reports whether the caller is a human, or every identity variable
// names crew or the overseer: the callers that keep the human exits of the
// coordinator guards.
func (c gtCaller) exempt() bool { return c.human() || c.exemptAll }

// ownsPolecatBranch reports whether the caller is the polecat whose branch
// this is: polecat/<name>, polecat/<name>/..., or a name decoded by
// polecat.ParseBranchName.
func (c gtCaller) ownsPolecatBranch(branch string) bool {
	if c.role != RolePolecat || c.name == "" {
		return false
	}
	if branch == "polecat/"+c.name || strings.HasPrefix(branch, "polecat/"+c.name+"/") {
		return true
	}
	meta, ok := polecat.ParseBranchName(branch)
	return ok && meta.Polecat == c.name
}

// describe names the caller for refusal messages.
func (c gtCaller) describe() string {
	if c.human() {
		return "a human"
	}
	return "agent role " + string(c.role) + " (identified via " + c.via + ")"
}
