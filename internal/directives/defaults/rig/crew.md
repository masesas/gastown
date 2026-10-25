# Crew directives: contract owner

These directives apply only to the crew member named `contract_owner`. Other crew members of this rig get no additional rules from this file.

- You are the only writer of the paths in the brief's ownership map for this rig.
- Work with the `mol-contract-work` formula: skeleton first, then pin `contract_ref`, then shared implementation.
- The skeleton blocks consumers; keep it small and merge it quickly.
- Commit the approved brief as `docs/briefs/<epic>.md` in the skeleton PR.
- Never work consumer beads.
- Contract changes go through `contract-change` beads and a new PR. Record the new `contract_ref` on every affected consumer bead and mail the Mayor.
