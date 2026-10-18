package beads

import (
	"errors"
	"fmt"
	"strings"
)

// ErrInvalidActor is returned by WithActor for an actor that is empty, longer
// than maxActorLen, starts with '-', or contains a control character.
var ErrInvalidActor = errors.New("invalid bd actor")

// maxActorLen bounds an actor: bd stores the assignee it is compared with in a
// VARCHAR(255) column.
const maxActorLen = 255

// WithActor returns a new wrapper, bound to the same database, whose bd
// operations all run as actor (surrounding whitespace trimmed): the bd
// subprocess gets BEADS_ACTOR=<actor> as its only identity (an inherited
// BEADS_ACTOR or BD_ACTOR is dropped, isolated wrappers included), creates
// pass --actor=<actor>, and the in-process store receives actor. bd's
// ownership guards compare against this actor: `bd close` refuses a bead
// assigned to anyone else (unless forced), and reassigning an in_progress
// bead held by someone else is refused. Wrappers derived from the result
// (prefix routing, ForAgentBead) keep the actor. The receiver is not changed.
//
// It is for a process that holds a claim under its own identity (a refinery
// merge run, gt-pd1c.93): it acts as the claimant rather than forcing past
// the guard. The actor is recorded in bd's audit trail as the writer.
func (b *Beads) WithActor(actor string) (*Beads, error) {
	actor = strings.TrimSpace(actor)
	if err := validateActor(actor); err != nil {
		return nil, err
	}
	return &Beads{
		workDir:    b.workDir,
		beadsDir:   b.beadsDir,
		isolated:   b.isolated,
		serverPort: b.serverPort,
		store:      b.store,
		townRoot:   b.townRoot,
		noRoute:    b.noRoute,
		actor:      actor,
	}, nil
}

func validateActor(actor string) error {
	switch {
	case actor == "":
		return fmt.Errorf("%w: empty", ErrInvalidActor)
	case len(actor) > maxActorLen:
		return fmt.Errorf("%w: longer than %d bytes", ErrInvalidActor, maxActorLen)
	case strings.HasPrefix(actor, "-"):
		return fmt.Errorf("%w: starts with '-'", ErrInvalidActor)
	}
	for _, r := range actor {
		if r < 0x20 || r == 0x7f {
			return fmt.Errorf("%w: contains a control character", ErrInvalidActor)
		}
	}
	return nil
}

// withActorEnv makes an explicit actor the bd subprocess's only identity.
func (b *Beads) withActorEnv(env []string) []string {
	if b.actor == "" {
		return env
	}
	env = StripEnvKey(StripEnvKey(env, "BEADS_ACTOR"), "BD_ACTOR")
	return append(env, "BEADS_ACTOR="+b.actor)
}
