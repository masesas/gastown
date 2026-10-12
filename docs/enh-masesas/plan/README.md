# Impl Plan Enhancement Gas Town

Spec: [`../spec/`](../spec/README.md). Setiap plan merujuk ID AC spec. Plan hanya berisi "bagaimana"; perilaku dan AC ada di spec.

| Plan | Spec | Bentuk kerja | Status |
| --- | --- | --- | --- |
| [P-S0](P-S0-prasyarat-pilot.md) | S0 | Operasional + 1 PR kode (upgrade Beads) | Siap dieksekusi |
| [P-S1a](P-S1a-kontrak-task-readiness.md) | S1a | 4 PR Go | Siap setelah Tahap 0 |
| [P-S1b](P-S1b-state-fail-closed.md) | S1b | 4 PR Go (PR-0 event + 3 PR) | Siap setelah Tahap 0 |
| [P-S1c](P-S1c-koordinasi-prosedural.md) | S1c | Formula, directives, pilot | Siap setelah Tahap 0 |
| P-S2a, P-S2b | S2a, S2b | — | Ditulis setelah gate Tahap 1 dan spike SP1 |

## Urutan eksekusi

1. **Tahap 0**: P-S0 Langkah 1–2 (baseline + upgrade Beads) lebih dulu; Langkah 3–8 setelahnya.
2. **Tahap 1** (paralel, file berbeda):
   - Jalur A: P-S1b PR-0 → PR-1 → PR-2 → PR-3.
   - Jalur B: P-S1a PR-1 → PR-2 ∥ PR-3 → PR-4. PR-0 S1b dan PR-1 S1a sama-sama menambah konstanta di `internal/events/events.go`; merge berurutan, rebase yang kedua.
   - Jalur C: P-S1c Langkah 1–5, lalu epic pilot (Langkah 6) **setelah** P-S1a PR-2 merged agar konsumen melihat kontrak penuh di prime.
3. **Gate Tahap 1** (`../11` § 1): semua AC S1a, S1b, S1c lulus.

## Aturan eksekusi

- Branch per PR dari `main` fork: `<type>/<spec>-<ringkas>` (mis. `fix/s1b-heartbeat-generation`).
- TDD: test RED di commit pertama PR, implementasi di commit berikutnya.
- Setiap PR: `make build`, test terarah, `go test ./...` dibanding baseline, review `go-reviewer` + `code-reviewer`, approval Anda.
- Pelacakan kerja memakai bead di `.beads` fork (satu bead per PR, dependensi mengikuti urutan di atas). Bead dibuat setelah Anda menyetujui plan ini.
- Perubahan perilaku yang ditemukan saat implementasi → perbarui spec dulu (bagian Riwayat), baru kode.
