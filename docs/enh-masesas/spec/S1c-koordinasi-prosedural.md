# S1c — Koordinasi Contract-First (Prosedural)

Asal: Rank 3P (`../09` § 4), protokol briefing (`../12` § 2.3, R1–R3, R5–R7), protokol pertanyaan dan asumsi (`../14`). Tahap 1. **Tanpa perubahan kode Go**: hanya formula baru di town (`<town>/.beads/formulas/`), directives, dan konfigurasi. Catatan: "overlay" di Gas Town hanya mengubah step formula yang sudah ada, jadi kedua formula di spec ini adalah formula baru, bukan overlay.

## 1. Masalah

- Konsumen kontrak (mis. mobile) bisa mulai sebelum kontrak BE final, sehingga bekerja dengan asumsi DTO lama.
- Dua polecat di satu rig bisa menulis area bersama yang sama (`shared/`, migrasi, DTO, komponen UI bersama), menghasilkan konflik atau duplikasi.
- Keputusan bisnis yang krusial bisa ditebak diam-diam oleh agent.

## 2. Perilaku yang diharapkan

### 2.1 Kapan briefing wajib

Epic wajib melewati briefing bila menyentuh salah satu: hot path (area bersama), kontrak lintas-rig, atau ≥ 3 bead paralel di rig yang sama. Epic lain langsung ke dispatch.

### 2.2 Formula `mol-contract-brief`

Langkah berurutan dengan hasil yang dapat diperiksa:

1. **Susun brief** (dijalankan Mayor). Draf disimpan di bead brief (label `gt:brief`) milik epic; versi yang disetujui di-commit ke repo rig pemilik kontrak (`docs/briefs/<epic>.md`) sebagai bagian PR skeleton, sehingga ikut di-pin `path@sha`. Mayor tidak menulis ke repo. Isi brief: Kontrak, Peta kepemilikan (`path-glob → bead owner`), Konsumen (bead → kontrak yang dipakai), Keputusan & alasan, Pertanyaan dan asumsi.
2. **Review paralel per area konsumen** (satu leg per rig/area konsumen). Setiap reviewer menjawab pertanyaan tetap: "Bisakah bead X diimplementasikan hanya dengan kontrak ini?" Output terstruktur `{bead, verdict: ok|gap|conflict, items[]}`. Maksimal 2 ronde.
3. **Cek konsistensi mekanis** (Mayor, berbentuk checklist yang wajib diisi):
   - setiap hot path yang disentuh epic punya tepat satu owner;
   - setiap bead konsumen bergantung (`blocks`) pada bead skeleton kontrak yang dipakainya;
   - tidak ada siklus dependensi;
   - setiap item kontrak punya AC di bead owner;
   - nomor migrasi direservasi dan tidak bentrok dengan `epic/*` lain yang terbuka.
4. **Approval Mayor**: Mayor menutup bead `gate` approval brief (bead gate diberi label `gt:keep` agar tidak ditutup otomatis oleh patrol stale). Syarat: review sudah berjalan (minimal satu ronde) dan semua cek mekanis lulus. Keberatan `conflict` yang tersisa setelah 2 ronde menjadi pertanyaan krusial (§ 2.4).

### 2.3 Formula `mol-contract-work` dan crew `contract-owner`

- Satu crew `contract-owner` per rig adalah satu-satunya penulis area dalam peta kepemilikan rig itu.
- Pekerjaan owner dipecah dua bead:
  - **skeleton kontrak** (endpoint/DTO/interface/migrasi kosong, API komponen): memblokir semua bead konsumen;
  - **implementasi shared**: tidak memblokir konsumen, kecuali brief menyatakan sebaliknya.
- Setelah skeleton merged ke `epic/<nama>`, owner mencatat `contract_ref = <repo>:<path>@<sha>` di notes bead skeleton dan bead konsumen (manual sampai S3).
- Konsumen yang menemukan kontrak kurang tidak mengubah area owner. Konsumen membuat bead `contract-change` untuk owner, dan berhenti di bagian yang terdampak.

### 2.4 Protokol pertanyaan dan asumsi (directives Mayor)

- Mayor memutuskan sendiri semua hal di luar tabel krusial, dan mencatatnya di bagian keputusan brief.
- Hal krusial (tabel `../14` § 2.2) menjadi bead berlabel `gt:question` dengan field: `id` (`Q-<epic>-<n>`), `class`, `question`, `context`, `options`, `assumed`, `affected`, `rework_cost`.
  - **`block`** (skema DB/migrasi, kontrak lintas-rig dengan konsumen > 1 rig, auth/RBAC/pembayaran/data pribadi, hapus/migrasi data, dependensi berbayar/lisensi tidak standar): task terkait diberi dependensi ke bead pertanyaan dan berhenti; pertanyaan dikirim segera via `gt mail send --human` dengan subjek `[BLOCK] Q-…`; pengingat setelah 24 jam.
  - **`proceed`** (lainnya): Mayor memilih asumsi, task berjalan dengan label `assumes:Q-<epic>-<n>` dan catatan asumsi di notes.
- **Propagasi manual**: saat men-dispatch task, Mayor menyalin label `assumes:*` dari semua blocker yang sudah selesai, dan memperbarui `affected` di bead pertanyaan.
- PR/MR task bertanda memuat bagian "Asumsi". Tanda tidak ditulis di kode.
- Pertanyaan `proceed` dikirim sebagai satu daftar paling lambat saat semua child epic merged. Format jawaban: `Q-id: <jawaban>` per baris. Diam tidak dianggap setuju.
- Setelah jawaban:
  - sama dengan asumsi → label dihapus, bead pertanyaan ditutup;
  - berbeda → Mayor membuat satu bead rework per bead bertanda (`discovered-from` ke bead asli, dependensi disalin, kontrak lebih dulu), dikerjakan polecat baru atau contract owner, melewati gate yang sama.
- PR epic → master tidak dibuat selama ada label `assumes:*` dengan pertanyaan terbuka atau bead rework belum merged.

### 2.5 Aturan dispatch

Bead konsumen hanya di-dispatch lewat `gt convoy stage` + `launch` atau scheduler. `gt sling` manual untuk bead konsumen dilarang sampai S3 (guard sling) selesai. Aturan ini ditulis di directives Mayor.

## 3. Acceptance criteria

- [ ] **S1c-AC1** Formula `mol-contract-brief`, `mol-contract-brief-review` (convoy review), dan `mol-contract-work` terpasang di `<town>/.beads/formulas/` pilot town dan lulus `gt formula run <nama> --dry-run` (validasi parser bawaan; tidak ada `gt formula validate`).
- [ ] **S1c-AC2** Directives Mayor memuat: kriteria briefing wajib, tabel krusial + kelas, format bead pertanyaan, aturan tanda + propagasi, aturan pengiriman/jawaban, aturan rework, larangan sling manual konsumen, dan larangan PR epic → master dengan asumsi terbuka.
- [ ] **S1c-AC3** Epic pilot BE + mobile dijalankan lewat brief: ada minimal satu ronde review terstruktur, checklist cek mekanis terisi dan lulus, bead gate ditutup Mayor.
- [ ] **S1c-AC4** Tidak ada bead konsumen yang di-dispatch sebelum skeleton kontraknya merged (diverifikasi dari timestamp bd).
- [ ] **S1c-AC5** Area dalam peta kepemilikan hanya diubah oleh commit dari crew `contract-owner` rig itu (diverifikasi dari `git log` per path di branch epic).
- [ ] **S1c-AC6** Setiap pertanyaan krusial di epic pilot tercatat sebagai bead `gt:question` dengan semua field; task terkait (termasuk dependen) berlabel `assumes:Q-*`; pertanyaan `block` terkirim dengan subjek `[BLOCK]`.
- [ ] **S1c-AC7** Simulasi jawaban: satu jawaban sama (label hilang, tanpa rework) dan satu berbeda (bead rework hanya untuk task bertanda, urutan kontrak dulu).
- [ ] **S1c-AC8** Formula baru dan directives dapat dilepas tanpa sisa (rollback diuji di town salinan).

## 4. Di luar scope

Kode Go apa pun (guard sling, feed fail-closed, `contract_ref` otomatis, `contract-stale`, guard kepemilikan, pengingat otomatis, propagasi otomatis, blokir PR epic otomatis): semuanya S3. Diskusi bebas multi-agent; role `contract-owner` di core; generator OpenAPI.

## 5. Risiko

| Risiko | Mitigasi |
| --- | --- |
| Mayor tidak patuh directives (LLM) | Checklist cek mekanis wajib terisi di brief; audit sampel oleh Anda; S3 mengotomasi bagian rawan |
| Owner menjadi bottleneck | Hanya skeleton yang memblokir; metrik waktu tunggu konsumen |
| Routing step ke crew via `workflow_target` belum teruji [I] | Diuji di S1c-AC3; fallback: Mayor sling skeleton ke crew dengan `--crew` |

## 6. Metrik

Pertanyaan krusial per epic; % jawaban berbeda dari asumsi; bead rework akibat asumsi; waktu tunggu `block`; konflik per MR; waktu tunggu konsumen terhadap skeleton; token dan durasi briefing.

## Riwayat

- 12 Okt 2026: dibuat dari Rank 3P, `12` § 2, dan `14`.
- 12 Okt 2026: koreksi istilah overlay → formula baru di town; validasi via `--dry-run`; label `gt:keep` pada bead gate (hasil verifikasi kode).
- 12 Okt 2026: lokasi brief: draf di bead `gt:brief`, versi final di repo lewat PR skeleton (Mayor tidak menulis repo).
- 12 Okt 2026: review brief dipisah ke formula convoy `mol-contract-brief-review` (leg paralel hanya tersedia di tipe convoy).
