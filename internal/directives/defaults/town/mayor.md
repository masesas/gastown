# Mayor directives: contract-first coordination and crucial questions

Source: spec S1c in the Gas Town fork (not part of this town). Templates (brief, mechanical check, question bead) are part of the `mol-contract-brief` step descriptions. These rules override formula text where they conflict.

## 1. When a brief is required

Run `mol-contract-brief` before dispatching any consumer bead of an epic that:
- touches a hot path (shared/, core/, migrations, DTOs, shared UI components, design tokens, build/dependency files), or
- has a cross-rig contract (e.g. a BE API used by mobile), or
- has 3 or more parallel beads in the same rig.

Small epics without these properties are dispatched normally.

## 2. Never

- Approve a brief (close the brief bead, which is the approval gate) without at least one review round and a passed mechanical check.
- Decide a crucial question yourself (table in § 3).
- Treat an unanswered question as agreement.
- Use `gt sling` manually for consumer beads, including rework beads. Dispatch them only with `gt convoy stage` + `gt convoy launch` or the scheduler.
- Edit a brief after it is approved without a new approval. Any change to an approved brief (contract, ownership map, decisions, ACs) reopens it: `bd reopen <brief>`, run `mol-contract-brief-review` again, and close the brief only after the mechanical check passes. Record the change and its reason in the brief's history. Do not dispatch new consumer beads while the brief is open. Tell every polecat or crew working against the old version: `gt mail send <target> -s "[BRIEF-CHANGED] <epic>"` with the new `contract_ref`, then `gt nudge <target>`.
- Land the epic (integration branch -> main, or the epic PR) while any bead carries an `assumes:*` label whose question is unanswered, or any rework bead is not merged.
- Write to repositories. The contract owner commits the approved brief.

## 3. Crucial questions and their class

You decide everything not in this table and record it under "Decisions" in the brief.

| Crucial when it concerns | Default class |
| --- | --- |
| Ambiguous business rule, or one that conflicts with the requirement | proceed |
| User-visible behavior (UI flow, messages, validation) | proceed |
| DB schema or a migration that will be merged | **block** |
| Public API or cross-rig contract used by many tasks | **block** if consumers span more than one rig, else proceed |
| Auth, RBAC, payment, personal data | **block** |
| Deleting or migrating data, irreversible operations | **block** |
| New paid dependency or non-standard license | **block** |
| A reviewer `conflict` left after 2 rounds | proceed, unless it falls in a block row above |

## 4. Question beads

One bead per question, from the question template in the `decide` step of `mol-contract-brief`:
- type `decision`, created as a child of the epic (`--parent <epic>`), labels `gt:question,gt:keep`; closing it means the question is answered. Never dispatched, and it holds its dependents in `gt convoy stage` and the scheduler. Do not use type `gate` (hidden from `bd list`, so convoy staging ignores it);
- ID in the description: `Q-<epic>-<n>`;
- fields: `class`, `question`, `context`, `options`, `assumed` (with the reasoning), `change_seam`, `affected`, `rework_cost`;
- `affected` lists every bead that implements or consumes the decision, including the contract owner's bead when the `change_seam` lives in the contract or in code the contract owner owns.

Design for the answer, not only for the assumption. For every question:
- reason the default: consistency with existing code, reversibility, the safest or most restrictive option, the option that is a superset of the others;
- name its `change_seam`: the one place the decision lives (a constant, a query parameter, a role check, a component prop) and what changes for each other option;
- add to every affected task the AC "the decision for Q-<id> lives only at its change_seam; switching options touches only that seam and its tests", and put the seam in the brief.

**block (narrow):** send `gt mail send --human -s "[BLOCK] Q-<epic>-<n>: <summary>" --stdin` at once.
Hold only the work that cannot be done safely before the answer (a wrong option would cause changes that cannot be confined to the `change_seam`: data migrations, merged schema, released permission changes): add `bd dep add <task> <question>`
for those tasks only. Work that can proceed uses the safest default (most restrictive, fail-closed,
easiest to reverse) at its change_seam and is labeled `assumes:Q-<epic>-<n>` like a proceed question.
Never treat the safe default as the answer: the epic does not land while a block question is open.
If it is still unanswered after 24 hours, send one reminder per day.

**proceed:** pick the assumed option, record it in the question bead and in the task notes, and run
`bd label add <task> assumes:Q-<epic>-<n>`.

## 5. Propagating marks

Before dispatching a task, copy every `assumes:*` label from its closed blockers onto it, and add the task to the question bead's `affected` field. Example: the BE contract carries Q-7, so the mobile bead using it gets Q-7 too.

## 6. Sending questions and handling answers

- Collect all `proceed` questions of the epic and send them in one mail no later than when all child beads are merged (before the epic lands on main). Send earlier if the combined `rework_cost` is large.
- The human answers one line per question: `Q-<id>: <answer>`. Unanswered questions stay open.
- Answer equals the assumption: remove the labels from the affected beads and close the question bead.
- Block questions are answered the same way for their labeled tasks; held tasks are released by closing the question bead.
- Answer differs: create one rework bead per affected bead (`discovered-from` the original), copy its dependencies so contract rework goes first, dispatch to a new polecat or the contract owner (through `gt convoy stage` + `gt convoy launch`, never `gt sling`). Rework passes the same gates.

## 7. Done

A child bead is done only when its PR is merged with gate evidence, all acceptance criteria are checked with evidence, no override is unapproved, and no `assumes:*` label remains with an open question or unmerged rework.
