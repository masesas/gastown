# Mayor directives: contract-first coordination and crucial questions

Source: spec S1c in the Gas Town fork (not part of this town). Templates: `$GT_ROOT/templates/contract/`. These rules override formula text where they conflict.

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
- Use `gt sling` manually for consumer beads. Dispatch them only with `gt convoy stage` + `gt convoy launch` or the scheduler.
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

One bead per question, from `$GT_ROOT/templates/contract/question.md`:
- type `decision`, created as a child of the epic (`--parent <epic>`), labels `gt:question,gt:keep`; closing it means the question is answered. Never dispatched, and it holds its dependents in `gt convoy stage` and the scheduler. Do not use type `gate` (hidden from `bd list`, so convoy staging ignores it);
- ID in the description: `Q-<epic>-<n>`;
- fields: `class`, `question`, `context`, `options`, `assumed` (proceed only), `affected`, `rework_cost`.

**block:** `bd dep add <task> <question>` for every affected task, then
`gt mail send --human -s "[BLOCK] Q-<epic>-<n>: <summary>" --stdin`.
If it is still unanswered after 24 hours, send one reminder per day.

**proceed:** pick the assumed option, record it in the question bead and in the task notes, and run
`bd label add <task> assumes:Q-<epic>-<n>`.

## 5. Propagating marks

Before dispatching a task, copy every `assumes:*` label from its closed blockers onto it, and add the task to the question bead's `affected` field. Example: the BE contract carries Q-7, so the mobile bead using it gets Q-7 too.

## 6. Sending questions and handling answers

- Collect all `proceed` questions of the epic and send them in one mail no later than when all child beads are merged (before the epic lands on main). Send earlier if the combined `rework_cost` is large.
- The human answers one line per question: `Q-<id>: <answer>`. Unanswered questions stay open.
- Answer equals the assumption: remove the labels from the affected beads and close the question bead.
- Answer differs: create one rework bead per affected bead (`discovered-from` the original), copy its dependencies so contract rework goes first, dispatch to a new polecat or the contract owner. Rework passes the same gates.

## 7. Done

A child bead is done only when its PR is merged with gate evidence, all acceptance criteria are checked with evidence, no override is unapproved, and no `assumes:*` label remains with an open question or unmerged rework.
