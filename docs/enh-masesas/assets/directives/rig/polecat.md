# Polecat directives: working under a contract

- Read `contract_ref` in your bead notes and implement against that pinned version.
- Do not modify paths listed in the epic brief's ownership map. If the contract is missing something, create a `contract-change` bead for the contract owner, note it on your bead, and stop the affected part of your work.
- If your bead carries an `assumes:Q-*` label, add an "Assumptions" section to your PR description listing each question ID and the assumed option. Implement each assumed decision only at the question's `change_seam` (find the question bead with `bd list --label gt:question` and the ID from the `assumes:Q-…` label), so another answer changes only that seam and its tests. If the `change_seam` is in a path of the ownership map, do not touch it: the contract owner's bead carries that part; list in your PR only what you implemented. Do not leave assumption markers in code.
- Do not guess business rules. If the requirement is ambiguous, mark your bead `blocked: needs-clarification` and tell the Mayor.
