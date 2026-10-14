# Baseline Metrik Pilot S0 (Gas Town tanpa enhancement S1c)

Tanggal: 13–14 Oktober 2026. Bukti S0-AC9 sisi Gas Town. Definisi metrik: `../11` § 3. Sampel kecil: median dan rentang, tanpa persentase.

## Lingkup

- Pilot town `$TOWN`, satu rig `posin` (monorepo k-posin: `apps/app-be-posin` Go, `apps/app-mobile-posin` Expo/TypeScript), remote = mirror lokal.
- Semua bead test-only (satu file test baru per bead, tanpa perubahan kode produksi). Kompleksitas rendah dan sebanding antar epic.
- Gate refinery: `posin-gate test` (BE: `make vet test`; mobile: `pnpm typecheck` + `pnpm test --ci`).
- Waktu dalam UTC. Lead time = `attached_at` (dispatch) sampai merge refinery ke target.

## Data per epic

| Epic | Agent polecat | Bead | Lead time | Gate pertama | Intervensi operator | Catatan |
| --- | --- | --- | --- | --- | --- | --- |
| kp-2lp | Claude (Opus) | 2 (BE, mobile) | tidak terukur | 1 dari 2 lulus; BE terputus nudge (infra), lulus saat diulang | ≥ 4: exclude worktree, guard `posin-gate`, Enter di dialog refinery, hentikan polecat duplikat | Dispatch ulang (gt-ka3d) mengaburkan timestamp; merge langsung ke `main` (alur epic belum ada) |
| kp-96c | Claude (Opus) | 3 (BE, mobile, BE) | 3 m, 4 m; 14 m (bead ke-3 ditahan operator ±10 m untuk uji gt-ka3d) | 3 dari 3 | 0 (di luar uji yang disengaja) | Alur `integration/{epic}` + land ke `main` |
| kp-dgt | agy (a1) | 1 (BE) | 7 m | 1 dari 1 | 0 | Bead tunggal ke `main` |
| kp-iwm | agy (a2, a3, a5) | 3 (BE) | tidak representatif (1–2 jam) | 2 dari 2 yang masuk gate | 0 | Laptop tidur ±08:45–10:40 WIB; bead ke-3 lolos ke `main` tanpa gate karena mayor memakai `--merge=direct` (gt-pd1c.37, sudah diperbaiki) |
| kp-7fb | agy (a1, a4) | 2 (BE, mobile) | 9 m, 6 m | 2 dari 2 | 0 | Setelah PR #17/#18; paralel dua akun |

## Ringkasan (run representatif: kp-96c bead 1–2, kp-dgt, kp-7fb)

| Metrik | Nilai |
| --- | --- |
| Lead time dispatch → merge | median 6 m, rentang 3–9 m (n = 5, bead test-only) |
| First-pass gate | 9 dari 10 MR yang masuk gate lulus di percobaan pertama; 1 gagal karena infrastruktur (gt-dzck), bukan kode |
| Rework loops | 0 |
| Konflik merge | 0 |
| False kill | 0 teramati |
| Escaped defects | 0 diketahui (perubahan hanya test) |
| Intervensi manusia | Turun dari ≥ 4 per epic (kp-2lp, sebelum perbaikan) ke 0 (kp-96c, kp-dgt, kp-7fb) |
| Verified throughput per minggu | Tidak bermakna untuk pilot sehari; diukur mulai epic S1c |

## Yang belum ada

- **Baseline workflow manual (Epic A di P-S0 Langkah 7).** Belum dijalankan; perlu epic sebanding yang dikerjakan secara manual. Lihat status S0-AC9 di spec.
- Metrik koordinasi, pertanyaan & asumsi, dan dampak limit akun baru relevan mulai S1c/S2.
