# Laporan Epic Pilot 2 — S1c Koordinasi Contract-First

Epic `kp-4az` "Daftar pelanggan (BE + mobile)" di pilot town `~/gt-pilot`, rig monorepo `posin` (k-posin lewat mirror lokal). Tanggal: 16 Oktober 2026. Bukti: catatan bead `gt-pd1c.24`, bead rig `kp-4az.*` (Dolt pilot `posin`), riwayat mirror `3340bbd..b207b42`. Spec: `../spec/S1c-koordinasi-prosedural.md`. Laporan epic 1: `epic-1-s1c.md`. Laporan ini ditulis 25 Oktober 2026 dari bukti tersimpan; catatan sesi operator asli sudah tidak ada.

## 1. Ringkasan

- **AC3–AC7 terpenuhi.** AC8 tidak diulang (sudah lulus di epic 1).
- **Aturan "block sempit" (spec § 2.4) terbukti:** pertanyaan `block` Q-kp-4az-1 terbuka sepanjang epic (17:15 → 19:01), tidak ada satu pun task yang ditahan, semua task kerja selesai dengan default paling aman, dan epic tidak di-land sebelum jawaban.
- **Hasil akhir:** `main` mirror `b207b42` = `integration/kp-4az` `b8ab229` + revert `f4c4e61`. Ukuran 34 file, +2986/−105 baris.
- **Lead time 2 jam 27 menit tanpa jeda**, intervensi operator 5 (target ≤ 5), brief 47 menit. Dibanding epic 1: lebih cepat dan jauh lebih sedikit intervensi, dengan infrastruktur yang sudah diperbaiki (agy-pool 6 akun, gate dengan DB, G1–G5).
- **Dua insiden integritas**, keduanya celah tool, bukan proses S1c:
  - leg review yang di-sling ulang tanpa `--review-only` membuat commit dan refinery me-merge-nya ke `main` mirror dalam 26 detik (`800d6ee`, direvert `f4c4e61`);
  - refinery me-land `integration/kp-4az` ke `main` sendiri walau `auto_land=false` (`b207b42`). Hasilnya benar, sehingga dipertahankan atas keputusan user.
- **Semua bug yang lahir dari epic ini sudah ditutup** (gt-pd1c.68–.72, .59, .60; PR #37–#40 dan lanjutan).

## 2. Kronologi (WIB, 16 Okt 2026)

| Waktu | Kejadian |
| --- | --- |
| 17:13–17:16 | Epic `kp-4az` + `integration/kp-4az` (`3340bbd`) dibuat; Mayor diberi mail + nudge 17:14:49 untuk `mol-contract-brief`. Brief `.1`, skeleton `.2`, shared `.3`, konsumen BE `.4` dan mobile `.5`, pertanyaan Q-1 (`block`), Q-2, Q-3 (`proceed`) dibuat 17:15 |
| 17:17 | Draft selesai; leg review r1: BE `kp-leg-hqa7e` (pipboy, a3), mobile `kp-leg-qoqr6` (brahmin, a4) |
| 17:26 | Leg BE r1 selesai: verdict `conflict` |
| 17:40 | **Intervensi #1:** kuota agy a4 habis di leg mobile (reset ±106 jam). Operator menandai a4 exhausted di pool dan meminta Mayor men-dispatch ulang lewat jalur normal (pool memilih akun lain), bukan Claude |
| 17:46–17:49 | Leg mobile r1 (dispatch ulang ke pipboy via `gt sling` polos) membuat commit docs `.brief-reviews/...` (`800d6ee`); refinery me-merge ke `main` mirror dalam 26 detik. **Intervensi #2:** operator revert `f4c4e61` (fast-forward, bukan force) |
| 17:50 | Sintesis r1 `kp-syn-axwgy` (BE conflict, mobile ok) → brief direvisi; leg r2: BE `kp-leg-54f2k` (pipboy) 17:50–17:58, mobile `kp-leg-3hzjk` (brahmin, a5) 17:50–18:02 |
| 18:01:48 | Brief `kp-4az.1` disetujui Mayor: checklist mekanis 5 butir pass (18:01:33), sintesis r2 `kp-syn-v3niu` overall ok (bead ditutup 18:02:42) |
| 18:03 | **Intervensi #3:** crew `contract_owner` dinyalakan operator atas permintaan Mayor (crew tidak auto-start, sama seperti epic 1) |
| 18:06–18:20 | Skeleton `.2` `e9cdf2b` (8 file, +624) merged, `.2` ditutup 18:18:32; shared `.3` `2ded70a` (6 file, +376/−105) merged, gate lulus dengan tes pg, `.3` ditutup 18:19:57 |
| 18:20–18:21 | Konsumen `.4` (pipboy, a3) dan `.5` (brahmin, a5) di-dispatch lewat `convoy stage --no-validate` + `launch` (edge convoy 18:20:17) |
| 18:40 | Mobile `.5` `4405ed2` (14 file, +947) merged ke integration; gate mobile 1667 tes lulus |
| 18:51 | BE `.4` `62456a4` (8 file, +1048/−49) merged; gate dengan tes pg lulus; `.4` ditutup 18:51:07 |
| 18:56 | **Intervensi #4:** convoy feeder me-sling ulang `.5` ke deathclaw karena bead tetap hooked setelah MR merged (gt-pd1c.70). Mayor menangkapnya sendiri (`[CEK]`); operator memberi diagnosis dan meminta `.5` ditutup + deathclaw dilepas |
| 18:59 | Jawaban user: Q-1 = B (beda dari asumsi A), Q-2 = A, Q-3 = A. Dikirim ke Mayor |
| 19:01 | **Intervensi #5:** operator menutup `.5` (loop re-sling berhenti). Q-1..Q-3 ditutup 19:01:18; rework `.9` (kontrak, contract_owner), `.10` (BE, deathclaw a6), `.11` (mobile, pipboy a3) dibuat 19:01:08; `.10`/`.11` `blocks` `.9` |
| 19:03–19:08 | Rework kontrak `.9` `ec977af` (4 file, +26/−13) merged; `.9` ditutup 19:08:23 |
| 19:19–19:35 | Rework BE `.10` `e4c9b38` (3 file, +51/−25) dan mobile `.11` `b8ab229` (3 file, +5/−4) merged paralel; integration = `b8ab229` |
| 19:41:21 | Refinery me-land `integration/kp-4az` ke `main` mirror sendiri (`b207b42`) walau `auto_land=false` dan instruksi "operator yang land" (gt-pd1c.71, P0). Dipertahankan atas keputusan user |
| 19:34–19:50 | Verifikasi independen operator pada `b8ab229`: BE semua lulus termasuk tes pg (skip hanya FCM/R2 live); mobile 3 FAIL timeout karena load mesin 95–99 (file tidak disentuh epic), diulang terpisah 76/76 lulus. Epic ditutup 19:49:46; `gt down`; 3 sesi dog tersisa → `kill-server` (G9) |

## 3. Acceptance criteria

| AC | Status | Bukti |
| --- | --- | --- |
| S1c-AC3 | Lulus | Brief `kp-4az.1` (`decision`, `gt:brief`, `gt:keep`) ditutup 11:01:48Z setelah 2 ronde: r1 `kp-syn-axwgy` (BE conflict → revisi), r2 `kp-syn-v3niu` (BE ok, mobile ok, overall ok). Checklist mekanis 5 butir pass dengan bukti perintah di notes brief (owner tunggal per path, dependensi konsumen → skeleton, tanpa siklus, AC per item kontrak, tanpa migrasi) |
| S1c-AC4 | Lulus | `bd dep`: `.4` → `.1`, `.2`, `.3`; `.5` → `.1`, `.2`. Konsumen di-dispatch 18:20:17 setelah `.2` ditutup 18:18:32 dan `.3` ditutup 18:19:57. Rework `.10`/`.11` `blocks` `.9` (ditutup 19:08:23) sebelum commit `.10` 19:19 dan `.11` 19:34 |
| S1c-AC5 | Lulus | `git log 3340bbd..b207b42` per path kontrak (`contracts/openapi.yaml`, `api/oapi-codegen`, `httpx/openapi`, `schema.d.ts`, `docs/briefs`, `report_range.go`, `customer_params.go`): hanya `contract_owner` (`e9cdf2b`, `2ded70a`, `ec977af`). `customer_stub.go` dibuat `.2` dan dihapus `.4` sesuai brief (preseden kp-rcu). Satu commit di luar `apps/`/`contracts/` (`800d6ee`, leg review) tertangkap operator dan direvert; tidak ada di tree akhir |
| S1c-AC6 | Lulus | 3 bead `decision` + `gt:question`, `gt:keep` dengan semua field (`id`, `class`, `question`, `context`, `options`, `assumed`, `change_seam`, `affected`, `rework_cost`). `affected` Q-1 memuat `kp-4az.2` (bead kontrak), perbaikan dari temuan epic 1 § 5.1 (4). `[BLOCK] Q-kp-4az-1` terkirim ke user |
| S1c-AC7 | Lulus | Sama (Q-2, Q-3): label `assumes:*` dicabut, bead ditutup, tanpa rework. Beda (Q-1): rework hanya untuk bead bertanda `.2`/`.4`/`.5` lewat `.9`/`.10`/`.11` (`discovered-from`), kontrak dulu (`.10`, `.11` `blocks` `.9`). Label `assumes:*` tersisa: 0 (tabel `labels`) |
| S1c-AC8 | Tidak diulang | Lulus di epic 1 pada aset yang sama |
| § 2.4 block sempit | **Terbukti** | Q-1 (`class: block`) terbuka 17:15 → 19:01. Tabel `dependencies`: tidak ada edge `blocks` ke `kp-4az.6`. Keempat task kerja selesai sebelum jawaban dengan default A (`auth.Secure(ownerOnly)`). Land 19:41 setelah jawaban 18:59 |

## 4. Metrik (definisi `../11` § 3)

| Metrik | Nilai |
| --- | --- |
| Biaya koordinasi: durasi brief | 47 menit (17:14:49 → 18:01:48), 2 ronde, 4 leg + 2 sintesis; token tidak terukur |
| Waktu tunggu konsumen terhadap skeleton/shared | 20 detik (`.3` ditutup 18:19:57 → dispatch 18:20:17); mobile 1 menit 45 detik setelah `.2`. Rework konsumen: 11 menit dari `.9` ditutup ke commit `.10` |
| Pertanyaan krusial | 3 (1 block, 2 proceed); 1 dari 3 berbeda dari asumsi (yang block) |
| Rework akibat asumsi | 3 bead: `.9` kontrak (4 file, +26/−13), `.10` BE (3 file, +51/−25), `.11` mobile (3 file, +5/−4). Total 10 file, ±124 baris, hanya di `change_seam` yang ditulis di pertanyaan |
| Waktu tunggu `block` | 1 jam 44 menit (17:15 → 18:59); 0 task tertahan |
| Lead time epic | 2 jam 27 menit wall-clock (17:14:49 → 19:41:21), tanpa jeda |
| MR merged | 7 (skeleton, shared, BE, mobile, 3 rework) + land; 0 konflik merge; tidak teramati gate menolak MR epic |
| Gate yang tidak membuktikan | Gate menjalankan tes pg (G5 selesai), tetapi tidak memeriksa batas path: 1 MR leg review (`800d6ee`, 76 baris docs) ter-merge ke `main`. Verifikasi akhir mobile dilakukan di bawah load mesin 95–99 (3 timeout, lulus saat diulang) |
| Intervensi operator | 5: kuota a4 (pool), revert `800d6ee`, start crew, diagnosis re-sling `.5`, close `.5` |
| Keputusan user | 3: pilihan epic (opsi A dari 3 kandidat), jawaban Q-1..Q-3, pertahankan land `b207b42` |
| Escaped defects | 0 diketahui pada fitur; 1 insiden integritas (`800d6ee`) tertangkap dan direvert dalam 3 menit |
| Ukuran hasil | 34 file, +2986/−105 (`3340bbd..b207b42`) |

### Perbandingan dengan epic 1 (`kp-rcu`)

| Metrik | Epic 1 `kp-rcu` | Epic 2 `kp-4az` |
| --- | --- | --- |
| Brief | 53 m, 2 ronde | 47 m, 2 ronde |
| Pertanyaan krusial | 6 (1 block, 5 proceed); 1 beda | 3 (1 block, 2 proceed); 1 beda |
| Rework | 2 bead (±84 baris) | 3 bead (±124 baris) |
| Lead time tanpa jeda | ±2 j 55 m (termasuk ±45 m gangguan spawn) | 2 j 27 m |
| Intervensi operator | ±30 | 5 |
| Konflik merge | 0 | 0 |
| Block sempit | belum teruji | terbukti |
| Polecat | agy a4 (1 akun) → Claude setelah kuota habis | agy-pool a3/a5/a6 (a4 habis, pool menggantikan) |
| Gate | tanpa tes pg; mobile lewat `pre_verified` | dengan tes pg; semua MR lewat gate |

Penurunan intervensi dari ±30 ke 5 sebagian besar hasil perbaikan G1–G5 dan agy-pool di antara dua epic, bukan perubahan proses S1c. Kedua epic memakai pola dan gate yang sama, tetapi fiturnya berbeda; perbandingan indikatif.

## 5. Temuan

### 5.1 Proses S1c

1. **Block sempit bekerja dan murah.** Pertanyaan RBAC terbuka 1 jam 44 menit tanpa menahan apa pun; jawaban yang berbeda dari asumsi hanya menyentuh guard, teks kontrak, dan tes (±124 baris, 3 bead, 33 menit dari jawaban ke merged).
2. **`affected` kini memuat bead kontrak.** Rework kontrak (`.9`) dibuat terpisah dan mendahului rework konsumen lewat `blocks`. Temuan epic 1 § 5.1 (4) tertangani oleh directives PR #30.
3. **Rework di-dispatch lewat convoy**, bukan `gt sling` (temuan epic 1 § 5.1 (3) tertangani). Satu-satunya `gt sling` polos adalah dispatch ulang leg review atas instruksi operator, dan itulah yang memicu `800d6ee`.
4. **Crew `contract_owner` masih harus dinyalakan operator** (intervensi #3, sama dengan epic 1). Kandidat: `gt up` menyalakan crew yang disebut di rig, atau Mayor menyalakannya sendiri saat brief disetujui.
5. **Bead sintesis r2 ditutup 54 detik setelah brief disetujui.** Verdict sudah ada di komentar SYNTHESIS, tetapi urutan tutupnya terbalik. Tidak berdampak; catatan untuk formula `mol-contract-brief-review`.

### 5.2 Gas Town (semua sudah ditutup)

| Bead | Temuan | Perbaikan |
| --- | --- | --- |
| gt-pd1c.71 (P0) | Refinery me-land integration ke `main` walau `auto_land=false` | PR #37 (land guard) |
| gt-pd1c.68 (P1) | Refinery/gate tidak memeriksa batas path; MR di luar scope epic ter-merge ke `main` | PR #38 (`merge_queue.allowed_paths`, `check-scope`) |
| gt-pd1c.69 (P1) | `gt sling` ulang pada leg review menghilangkan mode review-only | ditutup (lanjutan handoff 17 Okt) |
| gt-pd1c.70 (P1) | Bead sumber tetap hooked setelah MR merged → feeder me-sling ulang pekerjaan selesai | PR #39 (bukti merged dari git) |
| gt-pd1c.72 (P1) | Land guard lapis 2: pre-push hook saat `auto_land=false` | PR #40 (pushguard) |
| gt-pd1c.60 (P1) | G9 berulang: 3 sesi dog tersisa setelah `gt down` | ditutup |
| gt-pd1c.59 (P2) | G8 berulang: `.4` ditutup 18:51:07 sebelum merge | ditutup; waktu diukur dari commit mirror |

### 5.3 Agent dan lingkungan

- **agy-pool berfungsi sebagai pengganti kuota:** a4 habis 17:40 (reset ±106 jam), pool memilih a3/a5/a6 tanpa beralih ke Claude. Masih perlu satu intervensi operator untuk menandai a4 exhausted.
- **Load mesin 95–99** saat verifikasi akhir membuat 3 tes mobile timeout; bukan regresi (lulus saat diulang terpisah). Gate refinery berjalan di bawah load yang sama; hasilnya tetap lulus.
- **Instruksi operator "dispatch ulang dengan cara normal"** diterjemahkan Mayor sebagai `gt sling` polos. Kesalahan operator dan celah tool (gt-pd1c.69) terjadi bersamaan.

## 6. Batasan

- Token tidak terukur: agy tidak menyediakan angka; token Claude (Mayor, crew, refinery) tidak dikumpulkan.
- Laporan ditulis 9 hari setelah epic dari bead, Dolt pilot, dan riwayat mirror; catatan sesi operator asli sudah hilang. Jam intervensi dari catatan bead `gt-pd1c.24`.
- Verdict leg review tidak diverifikasi independen terhadap kode.
- `closed_at` bead polecat mendahului merge (G8); semua waktu merge diambil dari commit mirror.
- AC8 tidak diulang.
- Sampel dua epic dengan fitur berbeda; belum ada pembanding tanpa orkestrasi.

## 7. Langkah berikutnya (keputusan 25 Okt 2026)

1. **Hardening dibekukan.** gt-pd1c.91, .92, .94, .97, .98, .99, .100 berstatus `deferred`; dikerjakan hanya bila ada insiden nyata.
2. **Pembanding tanpa orkestrasi (3b):** epic setara dikerjakan satu sesi Claude bersama user, tanpa Gas Town, dengan metrik yang sama (jam aktif user, intervensi, token Claude dari transkrip) → kolom B di `../baseline/metrics-baseline.md`. Menggantikan rencana "epic manual penuh oleh user".
3. **Satu epic terukur dengan orkestrasi** (batas 6 jam aktif) setelah pembanding ada; fitur ditentukan user; tetap di mirror.
4. `gt-pd1c.24` tetap open sampai data pembanding terisi.
