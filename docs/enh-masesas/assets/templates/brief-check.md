# Brief mechanical check: <epic-id>

Each item needs the command run and the relevant output. A judgement without evidence does not count.

- [ ] **Single owner per hot path.** Every path glob in § 2 maps to exactly one owner bead, and every hot path changed by the epic's beads is covered.
  Evidence:
- [ ] **Consumers depend on their skeleton.** For each consumer bead, `bd dep list <bead>` shows the skeleton bead(s) it uses and the approval gate.
  Evidence:
- [ ] **No dependency cycle.** `gt convoy stage <epic> --dry-run` (or `bd blocked`) shows valid waves.
  Evidence:
- [ ] **Contract items have acceptance criteria.** Every row of § 1 appears as an AC item in its owner bead.
  Evidence:
- [ ] **Migration numbers reserved.** Reserved numbers do not exist on `main` or any open `epic/*` branch (`git ls-remote` / `git log --all -- <migration dir>`).
  Evidence:

Result: pass | fail (back to draft)
