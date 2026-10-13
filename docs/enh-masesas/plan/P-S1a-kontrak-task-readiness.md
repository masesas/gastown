# Impl Plan P-S1a — Kontrak Task Utuh + Readiness

Spec: [`../spec/S1a-kontrak-task-readiness.md`](../spec/S1a-kontrak-task-readiness.md). Empat PR, TDD. Baris dikonfirmasi dengan Read pada commit `649b832b`. Prasyarat: Tahap 0 selesai.

Urutan: PR-1 (config + helper murni) → PR-2 (render prime) ∥ PR-3 (readiness sling/scheduler) → PR-4 (validasi variabel formula).

## PR-1 — Setting `readiness` dan helper murni

| File | Perubahan |
| --- | --- |
| `internal/config/types.go` `RigSettings` (670-710) | `Readiness string \`json:"readiness,omitempty"\`` |
| `internal/config/loader.go` `validateRigSettings` (227-240) | Hanya `""`, `off`, `warn`, `enforce` |
| `internal/config/loader.go` (dekat `GetDefaultFormula` 2748-2758) | `GetReadinessMode(rigPath string) ReadinessMode`; error/kosong → `warn` |
| `internal/config/readiness.go` (baru) | `type ReadinessMode string`; konstanta `ReadinessOff/Warn/Enforce`; `const DefaultReadinessMode = ReadinessWarn` |
| `internal/beads/readiness.go` (baru, di samping `HasUncheckedCriteria` `beads.go:306-318`) | `CountCriteriaItems(ac string) int` (menghitung `- [ ]` dan `- [x]`, case-insensitive untuk `x`); `ReadinessProblems(issueType, description, ac string) []string` → `nil` bila siap atau tipe di luar {task, feature, bug}; deskripsi dibersihkan dari baris lampiran via `beads.ParseAttachmentFields` sebelum cek kosong |
| `internal/events/events.go` | `TypeTaskReadiness = "task_readiness"` + `TaskReadinessPayload(bead, rig, mode, outcome string, problems []string, actor, reason string)`; `outcome` ∈ `rejected`, `warned`, `overridden` |

Test (RED dulu):

| Test | File | AC |
| --- | --- | --- |
| `TestCountCriteriaItems` (tabel: kosong, teks bebas, `- [ ]`, `- [x]`, `- [X]`, campuran, indentasi) | `internal/beads/readiness_test.go` | dasar AC3 |
| `TestReadinessProblems` (tabel: siap; tanpa deskripsi; deskripsi hanya baris lampiran; tanpa AC; AC teks bebas; tipe `epic`/`gate`/`chore` → nil) | sama | AC3, AC9 |
| `TestRigSettingsValidation` + kasus readiness valid/tidak valid | `internal/config/loader_test.go` (307-404) | AC5 |
| `TestGetReadinessMode_DefaultWarn` (file tidak ada, field kosong) | `internal/config/loader_test.go` | AC4 |
| `TestRigSettingsSet` + `readiness enforce` | `internal/cmd/rig_settings_test.go` (171-486) | — |

## PR-2 — Render kontrak penuh di `gt prime` (S1a-AC1, AC2)

| File | Perubahan |
| --- | --- |
| `internal/cmd/prime_contract.go` (baru) | Fungsi murni `renderTaskContract(issue *beads.Issue, limits contractLimits) string`. Urutan bagian: Description, Acceptance Criteria, Design, Notes (slot `Contract Ref` disiapkan untuk S3, belum diisi). Bagian kosong dilewati. Baris lampiran mesin disaring dari Description. Pemotongan per bagian dan total, dengan penanda `[terpotong: N baris tidak ditampilkan — jalankan: bd show <id>]` |
| `contractLimits` | `{PerSectionLines int; TotalLines int}`; default konstanta bernama `defaultContractSectionLines = 200`, `defaultContractTotalLines = 400`; override via env `GT_PRIME_CONTRACT_MAX_LINES` (total) agar tidak butuh plumbing config di prime |
| `internal/cmd/prime.go` `outputHookedBeadDetails` (960-977) | Ganti potongan `maxLines := 5` dengan `renderTaskContract` |
| `internal/cmd/prime.go` `outputBeadPreview` (1123-1141) | Ganti potongan `maxLines := 12` dengan renderer yang sama (satu sumber perilaku) |
| Sumber data | Sebelum render, `b.Show(id)` (`beads.go:1556`) untuk mendapat design/notes/AC, karena `bd list --json` di `findAgentWorkOnce` (`prime.go:822-881`) mungkin tidak memuatnya. Error `Show` → render dari item list + baris peringatan "kontrak tidak lengkap: gagal membaca bead" (tidak diam) |

Test:

| Test | File | AC |
| --- | --- | --- |
| `TestRenderTaskContract_Full` (deskripsi 30 baris, 4 AC, design, notes → semua tampil berurutan) | `internal/cmd/prime_contract_test.go` | AC1 |
| `TestRenderTaskContract_TruncatesWithMarker` (per bagian dan total) | sama | AC2 |
| `TestRenderTaskContract_SkipsEmptyAndAttachmentLines` | sama | AC1 |
| `TestOutputHookedBeadDetails_UsesFullContract` (pola `captureStdout` 25-48 dan `TestCheckSlungWork_*` 943-975) | `internal/cmd/prime_test.go` | AC1 |
| `TestCheckSlungWork_ShowFails_WarnsIncompleteContract` | sama | AC1 |

Golden output: spec menyebut "golden"; repo tidak memakai file golden (tidak ada `testdata`). Dipakai **string ekspektasi lengkap** dalam test (setara golden, mengikuti konvensi repo).

## PR-3 — Readiness di sling dan scheduler (S1a-AC3..AC7, AC9, AC10)

### Fungsi inti

```go
// internal/cmd/readiness_guard.go
type readinessDecision struct {
	Allow    bool
	Outcome  string   // "", "warned", "rejected", "overridden"
	Problems []string
}

// checkReadiness never performs I/O besides reading the bead; callers emit events and print.
func checkReadiness(mode config.ReadinessMode, info *beadInfo, readErr error, force bool, reason string) (readinessDecision, error)
```

Aturan:

| Kondisi | `off` | `warn` | `enforce` |
| --- | --- | --- | --- |
| Siap | allow | allow | allow |
| Tidak siap | allow, tanpa event | allow, `warned` | `force && reason!=""` → allow, `overridden`; selain itu reject |
| `readErr != nil` | allow | allow, `warned` (problem `read_error`) | reject kecuali override (AC10) |
| `force && reason==""` | — | — | error "`--force-unready` butuh `--reason`" di semua mode (AC7) |

### Titik integrasi

| File | Perubahan |
| --- | --- |
| `internal/cmd/sling_helpers.go` `beadInfo` (93-101), `parseBeadInfo` (519) | Tambah `AcceptanceCriteria` (`acceptance_criteria`) |
| `internal/cmd/sling.go` | Flag `--force-unready` dan `--reason` (pola `slingForce` :124/:153). Cek setelah guard deferred (:637-639), sebelum `originalStatus` (:641). Rig ditentukan via `resolveRigForBead(townRoot, beadID)` (dipakai :415), karena target sling bisa crew/mayor |
| `internal/cmd/sling_dispatch.go` `SlingParams` (19-48), `executeSling` (:87) | Field `ForceUnready`, `UnreadyReason`; cek setelah guard deferred (:163-166). Jalur batch (`sling_batch.go`) ikut lewat sini |
| `internal/cmd/sling_schedule.go` `ScheduleOptions` (47-64), `scheduleBead` (68-225) | Field yang sama; cek setelah guard status (:127-129); set di ketiga call site (`sling.go:382, 423, 468`). Bead ditolak sebelum masuk antrean scheduler |
| `internal/scheduler/capacity/pipeline.go` | `SlingContextFields` (20-41), `DispatchParams` (216-231), `ReconstructFromContext` (234-254): `force_unready`, `unready_reason` (override tersimpan sampai dispatch) |
| `internal/cmd/capacity_dispatch.go` | `beadStatusInfo` (477-481) dan struct `items` (517-522) di `batchFetchBeadInfoByIDs` (497-546) + `description`, `acceptance_criteria`, `issue_type`. Filter di `readySlingContextsFromAssessments` (653-680) dengan log `dispatch_skip reason=not_ready` (pola `messaging_label` existing). **Tidak** lewat jalur `OnFailure` agar bead tidak kena circuit breaker `maxDispatchFailures` |
| Pencatatan override | `(*Beads).AddComment(id, "readiness override by <actor>: <reason>")` (`beads.go:2018`); actor via `detectActor()` (pola `sling_schedule.go:220`) |
| Event | `events.LogFeed(TypeTaskReadiness, …)`. Scheduler di daemon: pastikan penulisan event memakai town root eksplisit (cwd daemon tidak selalu di dalam town). Bila API events hanya `FindFromCwd`, tambah varian `LogFeedAt(townRoot, …)` |

Test:

| Test | File | AC |
| --- | --- | --- |
| `TestCheckReadiness` (tabel seluruh matriks di atas) | `internal/cmd/readiness_guard_test.go` | AC3–AC5, AC7, AC10 |
| `TestExecuteSling_NotReady_Enforce_Rejected_NoStateChange` (stub `writeBDStub` `sling_test.go:19-36`; stub mengembalikan `issue_type`, `acceptance_criteria`; assert tidak ada `bd update`) | `internal/cmd/sling_readiness_test.go` | AC3 |
| `TestExecuteSling_NotReady_Warn_Dispatched` | sama | AC4 |
| `TestExecuteSling_ForceUnready_RecordsComment` | sama | AC7 |
| `TestExecuteSling_EpicType_NotChecked` | sama | AC9 |
| `TestReadySlingContexts_SkipsNotReady_OthersDispatched` | `internal/cmd/capacity_dispatch_test.go` | AC6 |
| `TestScheduleBead_RefusesNotReady_Enforce` (tag `integration`) | `internal/cmd/scheduler_integration_test.go` | AC6 |
| `TestTaskReadinessPayload` | `internal/events/events_test.go` | AC4 |

## PR-4 — Validasi variabel formula sebelum cook (S1a-AC8)

| File | Perubahan |
| --- | --- |
| `internal/cmd/formula_vars_guard.go` (baru) | `validateFormulaTemplateVars(name, townRoot, rigName string, mode config.ReadinessMode) error`. Muat via `formula.ResolveFormulaContent(name, townRoot, rigName)` (`embed.go:55`) + `formula.Parse`, pola `resolveFormulaForRendering` (`prime_molecule.go:172`). Lakukan resolusi rantai extends/compose (`resolveChain`, `parser.go:514`) sebelum `ValidateTemplateVariables()` (`variable_validation.go:63-160`). Fallback prefix `mol-` seperti `verifyFormulaExists` (`sling_helpers.go:252-284`) agar file yang di-resolve sama dengan yang dipakai `bd cook` |
| Call site | `CookFormula` (`sling_helpers.go:1195-1217`), awal `InstantiateFormulaOnBead` (`sling_helpers.go:1028`), dan sebelum `BdCmd("cook")` di `sling_formula.go:463` |
| Mode | `off` lewati; `warn` peringatan + event; `enforce` return error |

Test:

| Test | File | AC |
| --- | --- | --- |
| `TestValidateFormulaTemplateVars_UndefinedVar_Enforce` (pesan memuat nama var) | `internal/cmd/formula_vars_guard_test.go` | AC8 |
| `TestValidateFormulaTemplateVars_Warn_Continues` | sama | AC8 |
| `TestEmbeddedFormulas_PassTemplateValidation` (iterasi semua formula embedded, termasuk yang extends/compose) | `internal/formula/variable_validation_test.go` | AC8 (tanpa false positive) |

Bila `TestEmbeddedFormulas_PassTemplateValidation` menemukan false positive, perbaiki validator (bukan formula), dan catat di PR.

## Verifikasi per PR

1. `make build`; `go vet ./...`.
2. Test terarah: `go test ./internal/beads ./internal/config ./internal/formula ./internal/events` dan `go test ./internal/cmd -run 'Readiness|RenderTaskContract|OutputHooked|CheckSlungWork|ExecuteSling_|ReadySlingContexts|FormulaTemplateVars'`. Di macOS ekspor flag ICU seperti `Makefile:24-31` bila menjalankan `go test` langsung.
3. `go test -tags integration ./internal/cmd -run ScheduleBead`.
4. `go test ./...` dibanding baseline Tahap 0.
5. Review `go-reviewer` → `code-reviewer`.
6. Pilot (setelah merge, mode `warn`): bead tanpa AC → peringatan + event; bead lengkap → `gt prime` polecat menampilkan AC penuh.

## Audit

| Layer | Hasil |
| --- | --- |
| 1 Tipe | `ReadinessMode` bertipe; `readinessDecision` eksplisit; fungsi murni terpisah dari I/O |
| 2 API | `AddComment`, `Show`, `ResolveFormulaContent`, `ValidateTemplateVariables` dipakai sesuai tanda tangan yang dikonfirmasi |
| 3 Error path | Error baca bead → fail-closed di `enforce` (AC10); error `Show` di prime → peringatan terlihat; error event tidak menggagalkan dispatch (best-effort seperti existing) |
| 4 Konsistensi | Setiap AC S1a ↔ test di tabel; istilah "item AC" = baris checkbox di semua tempat |
| 5 Contoh | Snippet `checkReadiness` konsisten dengan tabel aturan |
| 6 Default | `warn` (rig), 200/400 baris (prime), semua sebagai konstanta bernama |
| 7 Konkurensi | Tidak ada state bersama baru; scheduler membaca field tambahan dalam fetch batch yang sama |
| 8 Batasan | Di bawah |

## Batasan eksplisit

- **Out of scope:** penilaian kualitas AC; schema task baru; konfigurasi batas prime via file config (hanya env + default).
- **Known edge case:** bead lama dengan AC teks bebas akan diperingatkan (`warn`); naik ke `enforce` hanya setelah satu epic tanpa false reject.
- **Known edge case:** bead yang sudah di antrean scheduler sebelum upgrade tidak membawa field override; diperlakukan tanpa override.
- **Known limitation:** prime menambah token konteks; dibatasi oleh limit baris.

## Catatan eksekusi (13 Okt 2026)

Branch `feat/s1a-formula-vars` (rebase di atas `main` yang berisi Beads v1.3.0 dan S1b). PR-2 dan PR-3 dikerjakan paralel oleh dua agent di worktree terpisah, lalu disatukan linear.

| PR | Commit | Penyimpangan dari plan |
| --- | --- | --- |
| PR-1 | `bfdd9ae1` | `HumanDescription` menyaring baris metadata (termasuk `workflow_target`); tipe kosong dianggap `task` |
| PR-3 | `743c30f3` | `readySlingContextsFromAssessments` menerima `townRoot`; error baca bead di jalur sling sudah gagal lebih awal di semua mode (lebih ketat dari tabel); override menghasilkan komentar/event lagi saat dispatch; `events.LogFeedAt` ditambahkan |
| PR-4 | `3a375706` | Hanya `extends` yang di-resolve (hanya itu yang mewariskan `[vars]`); formula yang gagal diparse parser gt (mis. `security-audit`) hanya diperingatkan |
| PR-2 | `3089bd25`, `a04195e7` | Kontrak dirender sekali (preview hanya ID/judul/status); baris kosong dipertahankan |
| Review | `5791580d` | Fallback mode dari JSON mentah; aturan rig penentu mode; validasi formula sebelum spawn di `executeSling`/`gt sling` |

Keterbatasan diketahui: formula default `mol-polecat-work` tidak divalidasi saat `scheduleBead` (baru di-resolve saat dispatch, lalu divalidasi di `executeSling`); log `dispatch_skip reason=not_ready` berulang tiap siklus scheduler untuk bead yang tidak siap; batas prime berbasis baris, bukan byte.
