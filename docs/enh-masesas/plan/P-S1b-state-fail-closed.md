# Impl Plan P-S1b — State Fail-Closed di Boundary Lifecycle

Spec: [`../spec/S1b-state-fail-closed.md`](../spec/S1b-state-fail-closed.md). Tiga PR terpisah, masing-masing TDD, bisa di-revert sendiri. Urutan: PR-1 → PR-2 → PR-3 (PR-2 dan PR-3 berbagi helper tri-state). Semua baris di bawah sudah dikonfirmasi dengan Read/grep pada commit `649b832b`.

Prasyarat: Tahap 0 selesai (Beads sudah dinaikkan, baseline test diketahui).

## PR-0 — Event `state_unknown` (kecil, prasyarat PR-1..3)

| File | Perubahan |
| --- | --- |
| `internal/events/events.go` | Konstanta `TypeStateUnknown = "state_unknown"` di blok 37-76; builder `StateUnknownPayload(subject, check, reason, actor string) map[string]interface{}` mengikuti pola `SessionDeathPayload` (±300) |
| `internal/events/events_test.go` | Test payload berisi semua field |

## PR-1 — Heartbeat ber-generation (S1b-AC1..AC4)

**Akar masalah #4890:** `SessionManager.Start` (`internal/polecat/session_manager.go`) membuat sesi tmux di :481, tetapi baru menulis heartbeat di :605, setelah `WaitForRuntimeReady` (:521) dan nudge. Di jendela itu file heartbeat sesi lama (`exiting`) masih ada, dan reaper (`internal/daemon/daemon.go:2929-2931`) membunuh sesi baru.

**Sumber generation:** `runID := uuid.New().String()` sudah ada di `session_manager.go:447`, diekspor sebagai `GT_RUN` (:461).

### Perubahan

| File | Perubahan |
| --- | --- |
| `internal/polecat/heartbeat.go` | `SessionHeartbeat` (33-38) + `Generation string \`json:"generation,omitempty"\``. `TouchSessionHeartbeatWithState` (78-97) mengisi `Generation` dari `os.Getenv("GT_RUN")`. Fungsi baru `ReadSessionHeartbeatE(townRoot, session) (*SessionHeartbeat, error)`: `nil, nil` bila file tidak ada; `nil, err` bila tidak terbaca/rusak. `ReadSessionHeartbeat` (101-113) tetap ada sebagai wrapper (kompatibilitas) |
| `internal/polecat/session_manager.go` | Tepat sebelum `NewSessionWithCommandAndEnv` (:481): tulis heartbeat `state=working`, `Generation=runID` (fungsi baru `WriteSessionHeartbeat(townRoot, session, hb)` agar tidak bergantung env proses daemon). Penulisan di :605 tetap sebagai refresh |
| `internal/daemon/daemon.go` `reapIdlePolecat` (2906-2988) | Setelah baca heartbeat (:2916) pakai `ReadSessionHeartbeatE`. Error → event `state_unknown{check: heartbeat}` + `return` (tanpa kill). Ambil `GT_RUN` sesi via `tmux.GetEnvironment(session, "GT_RUN")`. Bila heartbeat `Generation != ""` **dan** `GT_RUN != ""` **dan** berbeda → anggap "belum ada heartbeat": jangan masuk cabang kill idle/exiting (2929-2931). Bila mismatch dan umur heartbeat ≥ 3× timeout → masuk cabang "working" existing (2938-2987), yang tetap mensyaratkan tanpa pekerjaan + `!IsAgentAlive`. Error `GetEnvironment` → `state_unknown`, `return` |
| `internal/cmd/tap_polecat_stop.go` (64-69) | Heartbeat `exiting/idle` dengan generation ≠ `GT_RUN` saat ini diabaikan (bug kelas sama) |
| `internal/witness/handlers.go` (1741-1767, 1970, 2302) | Audit saja di PR ini: bila keputusan kill memakai heartbeat, terapkan aturan generation yang sama lewat helper bersama `polecat.HeartbeatMatchesRun(hb, run string) bool`. Bila tidak memakai untuk kill, tidak diubah (dicatat di PR) |

Helper bersama (di `heartbeat.go`):

```go
// HeartbeatMatchesRun reports whether hb belongs to the session run identified by run.
// Legacy heartbeats (empty Generation) and sessions without GT_RUN are treated as matching,
// preserving pre-generation behavior.
func HeartbeatMatchesRun(hb *SessionHeartbeat, run string) bool {
	if hb == nil || hb.Generation == "" || run == "" {
		return true
	}
	return hb.Generation == run
}
```

### Test (ditulis dulu, RED)

| Test | File | AC |
| --- | --- | --- |
| `TestHeartbeatMatchesRun` (tabel: legacy, run kosong, sama, beda, nil) | `internal/polecat/heartbeat_test.go` | AC4 |
| `TestReadSessionHeartbeatE_MissingVsCorrupt` | sama | AC3 |
| `TestTouchSessionHeartbeat_WritesGeneration` (`t.Setenv("GT_RUN", …)`) | sama | AC1 |
| `TestReapIdlePolecat_StaleGenerationExiting_NotKilled` | `internal/daemon/polecat_health_test.go`; fake tmux (`writeFakeTmuxWithAgent` :387) diberi kasus `show-environment` → `GT_RUN=new` | AC1 |
| `TestReapIdlePolecat_CorruptHeartbeat_NotKilled` + event `state_unknown` | sama | AC3 |
| `TestReapIdlePolecat_LegacyHeartbeat_Unchanged` (tanpa generation → perilaku lama) | sama | AC4, AC9 |
| `TestReapIdlePolecat_MismatchOld3x_FallsToWorkingBranch` | sama | AC1 (risiko orphan) |
| `TestSessionManagerStart_WritesHeartbeatBeforeReadyWait` | `internal/polecat/session_manager_test.go`; butuh seam: urutan panggilan tmux direkam lewat fake yang sudah dipakai test Start (bila belum ada seam, tambahkan field fungsi `writeHeartbeat` pada `SessionManager` untuk observasi urutan) | AC2 |
| `TestTapPolecatStop_IgnoresOtherGeneration` | `internal/cmd/tap_polecat_stop_test.go` | AC1 |

## PR-2 — Lookup assignment tri-state (S1b-AC5, AC6, AC9)

### Tipe

```go
// internal/daemon/assignment.go
type WorkAssignment int

const (
	WorkUnknown WorkAssignment = iota // zero value is the safe one
	WorkNo
	WorkYes
)
```

Zero value sengaja `WorkUnknown` sehingga nilai yang lupa di-set tidak pernah berarti "aman untuk kill".

### Perubahan

| File | Perubahan |
| --- | --- |
| `internal/daemon/daemon.go` `hasAssignedOpenWork` (2827-2849) | Ganti menjadi `assignedOpenWork(rig, assignee) WorkAssignment`. Ada hasil di status mana pun → `WorkYes`. Semua query sukses tanpa hasil → `WorkNo`. Ada query gagal dan tidak ada hasil → `WorkUnknown` |
| `reapIdlePolecat` pemanggil di :2948 dan :2976 | `WorkYes` → tidak kill (seperti sekarang). `WorkUnknown` → event `state_unknown{check: assigned_work}`, increment counter, `return`. `WorkNo` → perilaku lama |
| Counter unknown | `map[string]int` per sesi di struct `Daemon`, dilindungi mutex yang sudah melindungi state reaper (bila tidak ada, `sync.Mutex` baru). Reset saat hasil `Yes/No`. Saat mencapai 3 → kirim satu mail ke witness rig dengan pola `notifyWitnessOfCrashedPolecat` (:2853); flag "sudah eskalasi" per episode agar tidak spam. Entri dihapus saat sesi hilang (cegah map tumbuh) |
| `internal/cmd/polecat.go` `hookBeadSafeForCleanup` (1345-1363) | `hookBead == ""` tidak lagi otomatis aman (field `hook_bead` tidak dipelihara lagi). Query assignee `<rig>/polecats/<name>` lewat helper tri-state versi `cmd` (lihat PR-3): `Yes` → blocker; `Unknown` → blocker `assigned_work=unknown`; `No` → aman |
| Pemanggil: `runPolecatCheckRecovery` (1103-1104), `checkPolecatSafety` (`polecat_helpers.go` 137, 175, 185-194) | Menyesuaikan tanda tangan; pesan blocker ditampilkan di dry-run nuke |

### Test

| Test | File | AC |
| --- | --- | --- |
| `TestAssignedOpenWork_TriState` (yes / no / semua gagal / sebagian gagal + ada hasil / sebagian gagal tanpa hasil) | `internal/daemon/has_assigned_work_test.go` (fake bd script) | AC5 |
| `TestReapIdlePolecat_BdListFails_NotKilled_EventLogged` (`writeFakeBDLookupFail` :414 + mode `list` exit 1) | `polecat_health_test.go` | AC5 |
| `TestReapIdlePolecat_UnknownThreshold_EscalatesOnce` (4 siklus → 1 mail) | sama | AC6 |
| `TestHookBeadSafeForCleanup_EmptyHook_UsesAssigneeQuery` | `internal/cmd/polecat_check_recovery_test.go` | AC5 |
| Test existing "no work → reap" (mis. 479-521) | tidak diubah, harus tetap hijau | AC9 |

## PR-3 — Guard kepemilikan `gt done` (S1b-AC7, AC8, AC9)

### Perubahan

| File | Perubahan |
| --- | --- |
| `internal/cmd/done.go` `queryAssignedBeads` (2570-2588) | Menjadi `([]*beads.Issue, error)`; error `bd.List` dikembalikan |
| `findAssignedBeadsForAgent` (2527-2568) | Menjadi `([]string, error)`. Error di satu lokasi tidak ditutupi "kosong" di lokasi berikutnya: bila ada error dan tidak ada hasil sama sekali → error |
| `findHookedBeadForAgent` (2610-2613) | Menjadi `(string, error)`; pemanggil `updateAgentStateOnDone` (:2314) mencatat warning (bukan jalur keputusan push) |
| `runDone` lookup hook (:889-913) | Error lookup → `gt done` gagal (sebelumnya jatuh ke "tidak ada issue") |
| Guard baru `verifyDoneOwnership(cwd, issueID, sender string) error` disisipkan **setelah :913, sebelum :915** | Memakai `resolveSubmitSourceIssue` (`source_validation.go:28-49`, sudah fail-closed pada error Show). Lulus bila `Assignee == sender` dan status bukan `closed` (menerima status apa pun yang belum closed, untuk jalur resume checkpoint :928/:1381). Assignee lain → error berisi assignee saat ini. Error lookup → error "ownership unknown". Event `state_unknown{check: done_ownership}` pada error |
| Cakupan | Wajib untuk status exit `COMPLETED`. `DEFERRED`/`ESCALATED` tidak melakukan push: guard hanya warning (keputusan eksplisit, dicatat di PR) |

Posisi guard dijamin sebelum semua efek samping jarak jauh: push langsung (:1223, :1323), push normal (:1409/:1420), dan `bd.Create` MR (:1757). Auto-commit lokal (789-841) terjadi sebelum guard; itu lokal dan tidak mengirim apa pun, dicatat sebagai edge case.

### Test

| Test | File | AC |
| --- | --- | --- |
| `TestVerifyDoneOwnership` (milik sendiri; milik lain; lookup error; status closed; status hooked/in_progress setelah resume) | `internal/cmd/done_ownership_test.go` (baru), stub bd pola `installSubmitSourceBDStub` (`source_validation_test.go:258`) | AC7, AC8 |
| `TestRunDone_NotAssignee_NoPushNoMR` | `internal/cmd/done_test.go` memakai harness `fakeDoneSessionKiller` (:994); assert: tidak ada panggilan push dan tidak ada create MR | AC7 |
| `TestRunDone_LookupError_NoPushNoMR` | sama | AC8 |
| `TestFindAssignedBeadsForAgent_ErrorNotMaskedByEmpty` | `done_test.go` | AC8 |
| `TestFindHookedBeadForAgent` existing | tetap hijau | AC9 |

## Verifikasi per PR

1. `go build ./...` dan `go vet ./...`.
2. `go test ./internal/polecat/... ./internal/daemon/... ./internal/cmd/... ./internal/events/... ./internal/witness/...`.
3. `go test ./...` dibandingkan baseline Tahap 0 (tidak ada kegagalan baru).
4. Review: `go-reviewer`, lalu `code-reviewer`.
5. Trace manual: reaper dari `reapIdlePolecats` → keputusan kill; `runDone` dari entry sampai push.
6. Di pilot town (setelah merge): sling ulang polecat yang baru `exiting` → sesi baru tidak di-kill; hentikan Dolt sesaat → tidak ada kill, event `state_unknown` muncul.

## Audit (rule implementation-plan, disesuaikan untuk Go)

| Layer | Hasil |
| --- | --- |
| 1 Tipe | `WorkAssignment` zero value = `Unknown`; tanda tangan baru mengembalikan `error` eksplisit; tidak ada `interface{}` baru di API publik selain payload event yang mengikuti pola existing |
| 2 API library | `uuid`, `tmux.GetEnvironment`, `events.LogFeed` dipakai sesuai pemakaian existing di file yang sama |
| 3 Error path | Setiap error baca → `unknown` → tanpa kill/push; counter dibersihkan saat sesi hilang |
| 4 Konsistensi | Setiap AC S1b punya test di tabel; istilah `generation` = `GT_RUN` = `runID` |
| 5 Contoh | Snippet `HeartbeatMatchesRun` lengkap dan bisa dikompilasi |
| 6 Default | Ambang eskalasi = 3 siklus (konstanta bernama `unknownEscalationCycles = 3`); mismatch-age = 3× timeout (sama dengan konstanta existing di :2955) |
| 7 Konkurensi | Counter unknown di daemon dilindungi mutex; heartbeat ditulis dengan rename atomik seperti writer existing (diverifikasi saat implementasi; bila belum atomik, tulis ke file sementara lalu `os.Rename`) |
| 8 Batasan | Lihat di bawah |
| 9 Portabilitas | Tidak relevan (bukan modul standalone) |

## Batasan eksplisit

- **Out of scope:** lease/fencing (E03); generation untuk sesi crew/dog (tidak punya `GT_RUN`, tetap jalur legacy).
- **Known edge case:** auto-commit lokal `gt done` terjadi sebelum guard kepemilikan; commit tetap ada di worktree lokal bila guard menolak (tidak di-push).
- **Known edge case:** outage `bd` berkepanjangan membuat polecat idle tidak pernah di-reap; ditangani dengan eskalasi, bukan kill.
- **Known limitation:** heartbeat legacy (tanpa generation) tetap rentan #4890 sampai sesi lama berganti; hilang dengan sendirinya setelah semua sesi restart.

## Catatan eksekusi (13 Okt 2026)

Branch `fix/s1b-state-fail-closed` (worktree `../gastown-s1b`), satu commit per PR:

| PR | Commit | Penyimpangan dari plan |
| --- | --- | --- |
| PR-0 | `7445fcd9` | — |
| PR-1 | `d8a6461a` | Ditemukan bug kedua: refresh heartbeat setelah runtime siap berjalan di proses pemanggil, sehingga generation-nya `GT_RUN` milik pemanggil. Kedua titik tulis memakai `claimSessionHeartbeat(runID)`. AC2 diuji di level helper + review urutan panggilan (tidak ada seam `Start` lengkap). Pembaca heartbeat witness hanya memakai heartbeat segar untuk menahan aksi, tidak diubah |
| PR-2 | `af2541b9` | Hanya reaper daemon. `hookBeadSafeForCleanup`/`p.Issue` di jalur nuke/recovery butuh analisis sumber `p.Issue`; dipindah ke `gt-pd1c.25` |
| PR-3 | (lihat git log) | Bead tanpa assignee diizinkan; guard hanya untuk `COMPLETED`; `ensureAgentBeadExists` (tulis agent bead) tetap terjadi sebelum guard, bukan efek remote |
