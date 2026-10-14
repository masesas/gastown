# Brief: <epic-id> — <epic title>

Status: draft | in review (round n) | approved (<date>; this bead closed)

## 1. Contract

| Item | Kind | Owner bead | Definition |
| --- | --- | --- | --- |
| `GET /api/v1/...` | endpoint | <skeleton-bead> | request, response, error codes, pagination |
| `<Name>Response` | DTO / response type | <skeleton-bead> | fields with type and nullability |
| `<nnnn>_<name>.sql` | migration (reserved number) | <skeleton-bead> | tables/columns |

## 2. Ownership map

| Path glob | Owner bead | Rig |
| --- | --- | --- |
| `<app>/internal/<shared-pkg>/**` | <owner-bead> | <rig> |

Every hot path the epic touches appears here exactly once.

## 3. Consumers

| Consumer bead | Rig | Uses contract items |
| --- | --- | --- |
| <bead> | <rig or app path> | `GET /api/v1/...`, `<Name>Response` |

## 4. Decisions and rationale

| Decision | Options rejected | Why |
| --- | --- | --- |

## 5. Questions and assumptions

| Q-id | Class | Question | Assumed (proceed) | Affected beads |
| --- | --- | --- | --- | --- |

## 6. Review log

| Round | Synthesis report | Result |
| --- | --- | --- |
