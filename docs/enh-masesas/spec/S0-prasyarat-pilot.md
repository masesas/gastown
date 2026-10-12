# S0 — Prasyarat Pilot

Asal: `../09` § 2 (P0.1–P0.4), keputusan D1-B, D2-A, D3, D5. Tahap 0.

## 1. Tujuan

Pilot Gas Town bisa berjalan di atas toolchain yang konsisten, terisolasi dari data existing, dengan konfigurasi yang sesuai stack `oto-rent-be` dan `oto-rent-pro`, serta ada baseline untuk mengukur manfaat enhancement.

## 2. Perilaku yang diharapkan

### S0.1 Toolchain

- `gt` dibangun dari fork (`make build`, flag ICU).
- `tmux` terpasang (Gas Town menjalankan agent di sesi tmux; belum terpasang di mesin ini per baseline).
- Beads yang di-embed fork sama versinya dengan `bd` CLI yang dipakai town (D2-A). Bila naik versi butuh > 2 hari, pilot berjalan dengan `bd` terisolasi lewat PATH khusus town (fallback C), dan upgrade dilanjutkan paralel.
- Baseline test fork diketahui: `go test ./...` hijau, atau setiap kegagalan tercatat dengan penyebab (environment vs bug).
- `gt daemon start` di pilot town tidak terblokir pemeriksaan versi store.

### S0.2 Pilot town terisolasi

- Town dibuat di direktori baru, bukan main checkout dan bukan `.beads` existing.
- Sebelum operasi apa pun, DB bd per-repo `oto-rent-be` dan `oto-rent-pro` di-backup, dan checksum/`bd count` dicatat.
- Rig pilot memakai DB town baru untuk epic baru saja (D1-B).
- Patrol yang berisiko destruktif ditinjau dan dimatikan sebelum daemon menyala: `wisp_reaper` (juga menjalankan auto-close stale 7 hari yang tidak bisa dimatikan terpisah) di-set `dry_run` atau off, dan `quota_dog` off. Bead yang boleh diam lama (gate, pertanyaan) diberi label `gt:keep`.

### S0.3 Konfigurasi rig

| Rig | Gate | Lainnya |
| --- | --- | --- |
| `be` | `make ci-local` (±4 menit), port dari `GT_POLECAT` ke `CI_PG_PORT` | — |
| `pro` | `fvm flutter analyze`, `fvm flutter test`, `build_runner` bila perlu | — |
| Keduanya | — | `merge_strategy=pr`; integration branch `epic/{title}`; branch polecat `feat/gt-{issue}` (variabel `polecat_branch_template` yang didukung: `{user} {year} {month} {name} {issue} {description} {timestamp}`; bukan `ai/*`); `role_agents` Claude untuk semua role; crew `contract-owner`. Pendaftaran akun Claude dipindah ke S2b, karena `gt account add` yang sekarang membuat config dir (melanggar I1) |

### S0.4 Baseline

- Satu epic kecil dikerjakan dengan workflow manual existing, satu dengan Gas Town tanpa enhancement.
- Metrik `../11` § 3 dicatat (minimal: verified throughput, first-pass gate rate, rework loops, intervensi manusia, konflik, lead time).

## 3. Acceptance criteria

- [ ] **S0-AC1** `gt version` di pilot menunjukkan commit fork; versi Beads embed dan `bd` CLI town tercatat dan cocok (atau fallback C aktif dan tercatat).
- [x] **S0-AC2** Hasil `go test ./...` tercatat dengan root cause setiap kegagalan (`../baseline/go-test-baseline.md`: 27 test, 4 kelompok).
- [ ] **S0-AC11** `tmux` terpasang; `go test ./...` dengan `TMPDIR` non-symlink hanya menyisakan kegagalan kelompok C (skew `bd`) sebelum upgrade Beads, dan 0 setelahnya.
- [ ] **S0-AC3** `gt daemon start` sukses di pilot town.
- [ ] **S0-AC4** Backup kedua DB per-repo ada dan uji restore ke direktori sementara berhasil.
- [ ] **S0-AC5** Checksum/`bd count` DB per-repo sama sebelum dan sesudah seluruh Tahap 0.
- [ ] **S0-AC6** Satu bead BE dan satu bead mobile melewati sling → polecat → refinery `pr` → PR ke `epic/...` dengan gate rig masing-masing.
- [ ] **S0-AC7** `wisp_reaper` dalam mode `dry_run` atau off, dan `quota_dog` off, dibuktikan dari `mayor/daemon.json` dan `disabled_patrols`.
- [ ] **S0-AC8** Crew `contract-owner` ada di kedua rig.
- [ ] **S0-AC9** Baseline metrik untuk dua epic tercatat di `../baseline/`.
- [ ] **S0-AC10** Catatan kompatibilitas ditulis: commit fork, versi bd, Beads embed, Dolt, Claude Code.

## 4. Di luar scope

Migrasi backlog existing (E15 penuh); retention/restore drill otomatis (E19 penuh); autodetect stack (E29); perubahan kode fork selain yang dibutuhkan upgrade Beads.

## 5. Risiko

| Risiko | Mitigasi |
| --- | --- |
| Upgrade Beads mengubah API/schema (#4793 [U]) | Timebox 2 hari; fallback C |
| Patrol upstream menutup/menghapus bead | S0-AC7 sebelum daemon menyala |
| Perilaku mode `pr` tanpa GitHub Actions di BE belum diuji | S0-AC6 menguji jalur penuh; temuan masuk S2a |

## Riwayat

- 12 Okt 2026: dibuat dari `09` § 2.
- 12 Okt 2026: patrol dan template branch dipertajam dari verifikasi kode (`wisp_reaper` memuat auto-close stale; variabel template `{issue}`).
- 12 Okt 2026: pendaftaran akun Claude dipindah ke S2b.
- 12 Okt 2026: baseline test: 27 kegagalan (bukan 2); tambah prasyarat tmux dan S0-AC11.
