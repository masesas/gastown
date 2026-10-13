# Brief: <epic-id> — <epic title>

Status: draft | in review (round n) | approved (gate <gate-id>, <date>)

## 1. Contract

| Item | Kind | Owner bead | Definition |
| --- | --- | --- | --- |
| `GET /api/v1/...` | endpoint | <skeleton-bead> | request, response, error codes, pagination |
| `TarifDto` | DTO | <skeleton-bead> | fields with type and nullability |
| `V0xx__...sql` | migration (reserved number) | <skeleton-bead> | tables/columns |

## 2. Ownership map

| Path glob | Owner bead | Rig |
| --- | --- | --- |
| `be/src/main/java/.../shared/**` | <owner-bead> | be |

Every hot path the epic touches appears here exactly once.

## 3. Consumers

| Consumer bead | Rig | Uses contract items |
| --- | --- | --- |
| <bead> | pro | `GET /api/v1/...`, `TarifDto` |

## 4. Decisions and rationale

| Decision | Options rejected | Why |
| --- | --- | --- |

## 5. Questions and assumptions

| Q-id | Class | Question | Assumed (proceed) | Affected beads |
| --- | --- | --- | --- | --- |

## 6. Review log

| Round | Synthesis report | Result |
| --- | --- | --- |
