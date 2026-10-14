# S0 — Prasyarat Pilot

Asal: `../09` § 2 (P0.1–P0.4), keputusan D1-B, D2-A, D3, D5. Tahap 0.

## 1. Tujuan

Pilot Gas Town bisa berjalan di atas toolchain yang konsisten, terisolasi dari data existing, dengan konfigurasi yang sesuai stack project target, serta ada baseline untuk mengukur manfaat enhancement.

Pilot dibagi dua fase:

- **Fase 1 (Tahap 0–1): k-posin.** Monorepo `k-posin` (`apps/app-be-posin` Go, `apps/app-mobile-posin` Expo/TypeScript) lewat **mirror lokal tanpa remote**. Project `oto-rent-*` tidak disentuh selama fase ini (keputusan Anda, 13 Okt 2026).
- **Fase 2 (setelah gate Tahap 1): `oto-rent-be` dan `oto-rent-pro`.** Rencana awal (dua rig, mode `pr` ke GitHub, backup DB per-repo) berlaku untuk fase ini dan dijalankan ulang dengan AC yang sama seperti versi 12 Okt (lihat Riwayat).

## 2. Perilaku yang diharapkan

### S0.1 Toolchain

- `gt` dibangun dari fork (`make build`, flag ICU) dan disalin ke direktori tooling pilot, bukan `make install` global.
- `tmux` terpasang (Gas Town menjalankan agent di sesi tmux).
- Beads yang di-embed fork sama versinya dengan `bd` CLI yang dipakai town (D2-A). Bila naik versi butuh > 2 hari, pilot berjalan dengan `bd` terisolasi lewat PATH khusus town (fallback C), dan upgrade dilanjutkan paralel.
- Dolt untuk town ≥ 2.0.7, dipasang terisolasi di tooling pilot bila Dolt sistem lebih tua; Dolt sistem yang dipakai project lain tidak diubah.
- Baseline test fork diketahui: `go test ./...` hijau, atau setiap kegagalan tercatat dengan penyebab (environment vs bug).
- `gt daemon start` di pilot town tidak terblokir pemeriksaan versi store.

### S0.2 Pilot town terisolasi

- Town dibuat di direktori baru, bukan main checkout dan bukan `.beads` existing.
- Repo target diakses lewat mirror lokal tanpa remote. Repo asli dan DB beads per-repo tidak pernah ditulisi. Bukti isolasi: HEAD repo asli dan daftar branch-nya tidak berubah, dan proses Dolt project lain tetap berjalan tanpa disentuh.
- Rig pilot memakai DB town baru untuk epic baru saja (D1-B).
- Config agent pilot terisolasi dari config global (`CLAUDE_CONFIG_DIR` salinan terkurasi; home terpisah per akun untuk agent lain) dan dilindungi guard yang menolak akses ke path di luar pilot.
- Patrol yang berisiko destruktif ditinjau dan dimatikan sebelum daemon menyala: `wisp_reaper` (juga menjalankan auto-close stale 7 hari yang tidak bisa dimatikan terpisah) di-set `dry_run` atau off, dan `quota_dog` off. Patrol yang menyentuh backup, remote, atau maintenance DB juga dimatikan selama fase 1. Bead yang boleh diam lama (gate, pertanyaan) diberi label `gt:keep`.

### S0.3 Konfigurasi rig

Fase 1:

| Rig | Gate | Lainnya |
| --- | --- | --- |
| `posin` (prefix `kp`) | `posin-gate test`: BE `make vet test`; mobile `pnpm typecheck` + `pnpm test --ci`. `posin-gate setup` menyiapkan dependensi | `merge_strategy=direct` (refinery merge sendiri; mirror lokal tidak punya API PR); integration branch `integration/{epic}`, `integration_branch_auto_land=false` (land manual oleh operator); crew `contract_owner` |

Fase 2: dua rig `be` (`make ci-local`, port dari `GT_POLECAT` ke `CI_PG_PORT`) dan `pro` (`fvm flutter analyze`, `fvm flutter test`, `build_runner` bila perlu), `merge_strategy=pr`, integration branch `epic/{title}`, branch polecat `feat/gt-{issue}`, crew `contract_owner` di kedua rig.

Berlaku untuk kedua fase: pendaftaran akun Claude dipindah ke S2b, karena `gt account add` yang sekarang membuat config dir (melanggar I1). Nama crew memakai garis bawah (`contract_owner`); `gt` menolak tanda hubung pada nama crew.

### S0.4 Baseline

- Satu epic kecil dikerjakan dengan workflow manual existing, satu atau lebih dengan Gas Town tanpa enhancement S1c.
- Metrik `../11` § 3 dicatat (minimal: verified throughput, first-pass gate rate, rework loops, intervensi manusia, konflik, lead time).

## 3. Acceptance criteria

Status fase 1 (14 Okt 2026). Bukti per AC ada di plan `../plan/P-S0-prasyarat-pilot.md` § 2.

- [x] **S0-AC1** `gt version` di pilot menunjukkan commit fork; versi Beads embed dan `bd` CLI town tercatat dan cocok (atau fallback C aktif dan tercatat).
- [x] **S0-AC2** Hasil `go test ./...` tercatat dengan root cause setiap kegagalan (`../baseline/go-test-baseline.md`: 27 test, 4 kelompok).
- [x] **S0-AC11** `tmux` terpasang; `go test ./...` dengan `TMPDIR` non-symlink hanya menyisakan kegagalan kelompok C (skew `bd`) sebelum upgrade Beads, dan 0 setelahnya.
- [x] **S0-AC3** `gt daemon start` sukses di pilot town.
- [x] **S0-AC4** (direvisi 14 Okt) Repo target diakses lewat mirror lokal tanpa remote; tidak ada operasi tulis ke repo asli maupun DB beads per-repo selama Tahap 0. Fase 2: backup kedua DB per-repo ada dan uji restore ke direktori sementara berhasil.
- [x] **S0-AC5** (direvisi 14 Okt) HEAD dan daftar branch repo asli sama sebelum dan sesudah Tahap 0, dan tidak ada commit pilot di repo asli. Fase 2: checksum/`bd count` DB per-repo sama sebelum dan sesudah.
- [x] **S0-AC6** (direvisi 14 Okt) Satu bead BE dan satu bead mobile melewati sling → polecat → refinery (gate rig) → merge ke integration branch epic → land ke `main`. Fase 2: refinery `pr` → PR ke `epic/...`.
- [x] **S0-AC7** `wisp_reaper` dalam mode `dry_run` atau off, dan `quota_dog` off, dibuktikan dari `mayor/daemon.json` dan `disabled_patrols` (atau default kode bila tidak dikonfigurasi).
- [x] **S0-AC8** (direvisi 14 Okt) Crew `contract_owner` ada di setiap rig pilot.
- [ ] **S0-AC9** Baseline metrik tercatat di `../baseline/`. Sisi Gas Town selesai (`../baseline/metrics-pilot-s0.md`); baseline workflow manual belum dijalankan.
- [x] **S0-AC10** Catatan kompatibilitas ditulis: commit fork, versi bd, Beads embed, Dolt, Claude Code (`../baseline/compat.md`).

## 4. Di luar scope

Migrasi backlog existing (E15 penuh); retention/restore drill otomatis (E19 penuh); autodetect stack (E29); perubahan kode fork selain yang dibutuhkan upgrade Beads dan bug yang menghalangi AC di atas.

## 5. Risiko

| Risiko | Mitigasi |
| --- | --- |
| Upgrade Beads mengubah API/schema (#4793 [U]) | Timebox 2 hari; fallback C |
| Patrol upstream menutup/menghapus bead | S0-AC7 sebelum daemon menyala |
| Mode `pr` tanpa GitHub Actions belum diuji (fase 1 memakai mirror lokal) | Diuji di fase 2; temuan masuk S2a |
| Agent menyentuh state di luar pilot | Config agent terisolasi + guard path; uji sintetis allow/deny |
| Versi agent eksternal berubah sendiri (Claude Code, `agy` auto-update) | Dicatat di `../baseline/compat.md` setiap tahap |

## Riwayat

- 12 Okt 2026: dibuat dari `09` § 2.
- 12 Okt 2026: patrol dan template branch dipertajam dari verifikasi kode (`wisp_reaper` memuat auto-close stale; variabel template `{issue}`).
- 12 Okt 2026: pendaftaran akun Claude dipindah ke S2b.
- 12 Okt 2026: baseline test: 27 kegagalan (bukan 2); tambah prasyarat tmux dan S0-AC11.
- 13 Okt 2026: fase 1 pilot memakai k-posin lewat mirror lokal; `oto-rent-*` tidak disentuh dan dipindah ke fase 2 (keputusan Anda). Akibatnya backup DB per-repo (P-S0 Langkah 3) tidak dijalankan di fase 1.
- 13 Okt 2026: Dolt town ≥ 2.0.7 dipasang terisolasi di tooling pilot (gt-pd1c.29).
- 14 Okt 2026: rig fase 1 memakai `merge_strategy=direct` dan `integration/{epic}` karena remote mirror lokal tidak punya API PR; nama crew `contract_owner` karena `gt` menolak tanda hubung. S0-AC4, AC5, AC6, AC8 direvisi untuk fase 1; versi asli berlaku di fase 2.
- 14 Okt 2026: status fase 1: semua AC lulus kecuali S0-AC9 (baseline manual). Temuan dan PR perbaikan dicatat di plan P-S0 § 4.
