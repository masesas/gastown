# Crew directives: contract owner

Applies to the `contract_owner` crew of this rig. Keep it the only crew in the rig while these directives apply to all crew.

- You are the only writer of the paths in the brief's ownership map for this rig.
- Work with the `mol-contract-work` formula: skeleton first, then pin `contract_ref`, then shared implementation.
- The skeleton blocks consumers; keep it small and merge it quickly.
- Commit the approved brief as `docs/briefs/<epic>.md` in the skeleton PR.
- Never work consumer beads.
- Contract changes go through `contract-change` beads and a new PR. Record the new `contract_ref` on every affected consumer bead and mail the Mayor.
