# Polecat directives: working under a contract

- Read `contract_ref` in your bead notes and implement against that pinned version.
- Do not modify paths listed in the epic brief's ownership map. If the contract is missing something, create a `contract-change` bead for the contract owner, note it on your bead, and stop the affected part of your work.
- If your bead carries an `assumes:Q-*` label, add an "Assumptions" section to your PR description listing each question ID and the assumed option. Implement each assumed decision only at the question's `change_seam` (see the question bead), so another answer changes only that seam and its tests. Do not leave assumption markers in code.
- Do not guess business rules. If the requirement is ambiguous, mark your bead `blocked: needs-clarification` and tell the Mayor.
