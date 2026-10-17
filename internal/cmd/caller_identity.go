package cmd

import "strings"

// gtCaller is who runs a guarded Gas Town action, read from the agent identity
// variables (agentSessionEnvVars). It is the one classification used by the
// coordinator guards (gt-pd1c.78): the pre-push guard, gt polecat nuke --force
// and the manual landing verdict. It is a guard against agents taking a
// human's exit by mistake, not an authentication boundary: anyone can change
// their environment.
type gtCaller struct {
	// role is the parsed role (parseRoleString); "" for a human. A value that
	// is not a known Role is kept as is and counts as an agent (fail closed).
	role Role
	// via names the variable that identified an agent; "" for a human.
	via string
	// actor is BD_ACTOR, trimmed (may be empty).
	actor string
}

// gtCallerFrom classifies the caller. GT_ROLE decides the role, then
// BD_ACTOR, then GT_POLECAT, then GT_CREW. A polecat identity anywhere
// (GT_POLECAT set, or a <rig>/polecats/<name> BD_ACTOR) wins over a crew
// GT_ROLE: a polecat must not pass as crew.
func gtCallerFrom(getenv func(string) string) gtCaller {
	c := gtCaller{via: agentSessionEnvVar(getenv), actor: strings.TrimSpace(getenv("BD_ACTOR"))}
	if c.via == "" {
		return c
	}
	if strings.TrimSpace(getenv("GT_POLECAT")) != "" || isPolecatActor(c.actor) {
		c.role = RolePolecat
		return c
	}
	if env := strings.TrimSpace(getenv(EnvGTRole)); env != "" {
		c.role, _, _ = parseRoleString(env)
		return c
	}
	if c.actor != "" {
		c.role, _, _ = parseRoleString(c.actor)
		return c
	}
	c.role = RoleCrew // only GT_CREW is set
	return c
}

// roleOverseer is the identity a human overseer uses (BD_ACTOR=overseer, the
// mail identity of a human at a terminal). It is not an agent role.
const roleOverseer Role = "overseer"

// human reports whether no agent identity is set.
func (c gtCaller) human() bool { return c.via == "" }

// exempt reports whether the caller is a human (no identity, or the overseer
// identity) or a crew workspace: the callers that keep the human exits of the
// coordinator guards.
func (c gtCaller) exempt() bool {
	return c.human() || c.role == RoleCrew || c.role == roleOverseer
}

// describe names the caller for refusal messages.
func (c gtCaller) describe() string {
	if c.human() {
		return "a human"
	}
	return "agent role " + string(c.role) + " (identified via " + c.via + ")"
}
