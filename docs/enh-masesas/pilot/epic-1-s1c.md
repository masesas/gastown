# Laporan Epic Pilot 1 — S1c Koordinasi Contract-First

Epic `kp-rcu` "Laporan produk terlaris" (BE + mobile) di pilot town `~/gt-pilot`, rig monorepo `posin` (k-posin lewat mirror lokal). Tanggal: 14–15 Oktober 2026. Bukti lengkap: catatan bead `gt-pd1c.24` dan `gt-af2e`. Spec: `../spec/S1c-koordinasi-prosedural.md`. Plan: `../plan/P-S1c-koordinasi-prosedural.md` (Langkah 6–9).

## 1. Ringkasan

- **AC3–AC8 terpenuhi**, dengan dua catatan: AC5 punya satu pengecualian terencana, dan AC8 diuji di pilot sendiri, bukan di town salinan.
- **Aturan "block sempit" belum teruji**, karena satu-satunya pertanyaan `block` dijawab sebelum ada pekerjaan yang tertahan.
- **Hasil akhir:** `main` mirror `5eb3b81`, isi identik dengan `integration/kp-rcu` `a866ca8`.
- **Verifikasi operator, di luar gate refinery:**
  - tes Postgres BE di integration: 720 lulus, 0 gagal, 37 skip karena butuh Redis;
  - gate mobile: 172 suite, 1640 tes lulus;
  - saat land, tes di `main` hasil merge lulus.
- **Repo k-posin asli tetap di `4cfdde0`**, tanpa branch pilot.
- **Alur S1c bekerja seperti yang dirancang:** brief, review dua ronde, cek mekanis, skeleton lebih dulu, konsumen ditahan, lalu jawaban dan rework yang hanya menyentuh titik ubah.
- **Biaya terbesar justru infrastruktur, bukan koordinasi:**
  - kuota agy habis pukul 00:01, lalu epic diam semalaman (5 jam menunggu keputusan user dengan laptop menyala, sisanya laptop tidur);
  - spawn Claude gagal sementara;
  - pemulihan dispatch lewat convoy tidak punya jalur yang bersih.

## 2. Kronologi (WIB)

| Waktu | Kejadian |
| --- | --- |
| 14 Okt 22:48–22:50 | Pilot dinyalakan; epic `kp-rcu` + `integration/kp-rcu` dibuat; Mayor diberi tugas `mol-contract-brief` |
| 22:51 | Draft: brief `kp-rcu.1`, skeleton `.2`, parser `.3`, konsumen BE `.4` dan mobile `.5`, pertanyaan Q-1..Q-3; mail `[BLOCK] Q-kp-rcu-2` (RBAC) |
| 22:52–22:59 | Operator meminta alasan + `change_seam` + AC isolasi per pertanyaan (keputusan user); Q-2 dijawab A (owner saja) 22:57, ditutup 22:59 |
| 23:00 | Review r1 menambah Q-4..Q-6 (proceed) |
| 23:02–23:20 | Leg review r2 (agy) tertahan prompt izin ±17 menit |
| 23:43 | Brief disetujui: review r2 ok untuk `.4` dan `.5`, checklist mekanis pass |
| 23:44–23:54 | Crew `contract_owner` dinyalakan operator; skeleton `.2` merged (`9045e24`) |
| 23:55–23:59 | Mobile `.5` di-dispatch (23:55:35); parser `.3` merged (`b0c5b21`, 23:58:51); BE `.4` di-dispatch (23:59:15) |
| 15 Okt 00:01 | Kuota agy-a4 habis ("reset 148 jam", kuota mingguan) setelah ±1 jam pemakaian intensif (5 sesi agy sejak 23:00; a4 juga dipakai di epic kp-7fb); kedua konsumen berhenti tanpa commit; Mayor mengirim `[KEPUTUSAN]` |
| 00:01–05:02 | Laptop menyala (heartbeat daemon tiap 3 menit tanpa celah); pilot menunggu keputusan user |
| 05:02–14:22 | Laptop tidur (`pmset`: "Clamshell Sleep" 05:02:55, bangun karena lid dibuka 14:22:43); hanya DarkWake maintenance |
| 14:22–15:32 | Laptop menyala, sesi belum dilanjutkan |
| 15:34–15:45 | User memilih Claude; BE dilepas dan di-dispatch ulang lewat convoy; BE merged (`9928133`) |
| 15:37–16:02 | Spawn mobile gagal 4×: fork gagal, feed convoy gagal (formula), respawn limit, sesi Claude exit 1 (juga `hq-boot`) |
| 16:03–16:05 | Alarm stall → `gt down`; user menjalankan `claude -p` (rc=0); `gt up` bersih |
| 16:33–16:41 | Mobile di-dispatch ulang, merged (`4404041`) |
| 16:46 | Jawaban simulasi: Q-1 = B (beda), Q-3..Q-6 = A (sama) |
| 16:54–17:10 | Rework kontrak `.12` merged (`ee60a68`), rework BE `.13` merged (`a866ca8`); Q-1 ditutup |
| 17:12–17:18 | Uji rollback; land ke `main` (`5eb3b81`); pilot dimatikan |

## 3. Acceptance criteria

| AC | Status | Bukti |
| --- | --- | --- |
| S1c-AC3 | Lulus | Brief `kp-rcu.1` ditutup Mayor 16:42:58Z "approved: review r2, check ok". Sintesis r1 `kp-syn-hegbe` (NOT ok → revisi), r2 `kp-syn-a7quo` (`.4` ok, `.5` ok). Checklist 5 butir di notes brief, masing-masing dengan bukti perintah |
| S1c-AC4 | Lulus | Skeleton `.2` closed 16:54:32Z < mobile `.5` attached 16:55:35Z; parser `.3` closed 16:58:51Z < BE `.4` attached 16:59:15Z; rework `.12` closed 09:54:00Z < `.13` attached 10:03:49Z (UTC). Dispatch ulang 15 Okt juga sesudah semua blocker |
| S1c-AC5 | Lulus, 1 pengecualian | `git log 0d6368c..integration/kp-rcu` per path owner: kontrak, file generate, parser, config codegen, tes audit, dan brief hanya diubah `contract_owner`. `report_products_stub.go` dihapus `nuka` (BE), sesuai brief ("dihapus oleh kp-rcu.4") |
| S1c-AC6 | Lulus | 6 pertanyaan krusial sebagai bead `decision` + `gt:question,gt:keep` dengan semua field (termasuk `change_seam`); task terdampak berlabel `assumes:Q-*`; Q-2 `block` terkirim `[BLOCK] Q-kp-rcu-2` |
| S1c-AC7 | Lulus | Sama (Q-3..Q-6): label dilepas, pertanyaan ditutup, tanpa rework. Beda (Q-1): rework hanya untuk bead bertanda, kontrak dulu (`.12` → `.13`); mobile tanpa rework karena tidak mengirim limit (bukti di notes). Label `assumes:*` tersisa: 0 |
| S1c-AC8 | Lulus (di pilot, bukan town salinan) | Aset dilepas → `gt prime --dry-run --explain` untuk Mayor, crew, polecat: "no directive files found"; 0 formula contract; `mol-polecat-work` dry-run normal. Dipasang ulang dari `assets/` → output prime identik dengan baseline; 3 formula dry-run rc=0 |

## 4. Metrik (definisi `../11` § 3)

| Metrik | Nilai |
| --- | --- |
| Biaya koordinasi: durasi brief | 53 menit (22:50 → 23:43), 2 ronde review; token tidak terukur |
| Waktu tunggu konsumen terhadap skeleton/parser | mobile 63 detik setelah skeleton merged; BE 24 detik setelah parser merged |
| Pertanyaan krusial | 6 (1 block, 5 proceed); 1 dari 5 proceed berbeda dari asumsi |
| Rework akibat asumsi | 2 bead (`.12` kontrak + parser, `.13` BE); mobile 0. Masing-masing 1 commit kecil di titik ubah (6 file ±31 baris; 4 file ±53 baris) |
| Waktu tunggu `block` | 6 menit (22:51 → 22:57) |
| Lead time epic | 18 jam 28 menit wall-clock. Jeda 00:01–15:34: ±5 jam menunggu keputusan user (laptop menyala), ±9 jam 20 menit laptop tidur, ±1 jam 10 menit sebelum sesi dilanjutkan. Tanpa jeda itu ±2 jam 55 menit, termasuk ±45 menit gangguan spawn |
| MR merged | 6 (skeleton, parser, BE, mobile, 2 rework); 0 konflik merge; tidak teramati gate yang menolak |
| Gate yang tidak membuktikan | Gate refinery tidak menjalankan tes Postgres (`TEST_DATABASE_URL` kosong); MR mobile masuk lewat `pre_verified` tanpa gate refinery. Keduanya diverifikasi operator secara manual |
| Intervensi operator | ±30: menyalakan crew, ±10 persetujuan prompt agy, 3× reset respawn, 1× `gt down`/`gt up`, 2× tes pg, 1× gate mobile, instruksi pemulihan dispatch, nudge `.13` |
| Keputusan user | 9: pilihan epic, scope kontrak, tempat manual, kebijakan block + `change_seam`, Q-2, runtime agy, kebijakan prompt, runtime Claude, jawaban simulasi |
| Escaped defects | 0 diketahui |

Perbandingan dengan epic manual B: `../baseline/metrics-baseline.md` (kolom manual belum diisi).

## 5. Temuan

### 5.1 Proses S1c

1. **Aturan block sempit belum teruji.** Q-2 dijawab sebelum ada pekerjaan yang tertahan, dan saat itu Mayor masih memakai block penuh. Uji di epic berikutnya dengan membiarkan satu pertanyaan block terbuka.
2. **Brief diubah dua kali setelah disetujui, tanpa review ulang.** Pertama, pemecahan file ownership setelah r2. Kedua, permintaan crew (stub dibungkus `auth.Secure`, satu baris rute golden). Kontrak tidak berubah, tetapi directives belum mengatur perubahan pasca-approval.
3. **`kp-rcu.13` (rework konsumen) di-dispatch dengan `gt sling`,** melanggar directives "konsumen hanya convoy". Urutannya tetap benar. Mayor mengakuinya sendiri. Ini contoh risiko kepatuhan LLM yang perlu ditangani guard sling di S3.
4. **Field `affected` pertanyaan tidak memuat bead pemilik kontrak**, walaupun `change_seam` menyentuh kontrak. Akibatnya Mayor menggabungkan rework kontrak ke rework parser. Template perlu menyebutkannya.
5. **Titik ubah terbukti bekerja.** Rework Q-1 hanya menyentuh konstanta, kontrak, dan satu `ORDER BY`, sesuai brief. Mobile benar-benar tidak perlu diubah.

### 5.2 Gas Town (kandidat bead perbaikan)

| # | Temuan | Dampak |
| --- | --- | --- |
| G1 | `gt sling mol-convoy-feed deacon/dogs --var convoy=<id>` gagal: "validation failed for issue : title is required" | Convoy terdampar tidak bisa diberi makan secara manual |
| G2 | Convoy yang sudah open tidak bisa di-launch ulang (`transitionConvoyToOpen`), padahal launch membuka convoy sebelum dispatch | Bead yang gagal spawn di wave 1 terdampar; pemulihannya: tutup convoy lalu stage ulang, berulang kali |
| G3 | Penghitung respawn menghitung kegagalan infrastruktur (kuota, fork, sesi) | Butuh `respawn-reset` manual (3×) |
| G4 | Dispatch ulang bersamaan dengan pembersihan witness memilih nama polecat yang sedang dihapus | Spawn gagal, membingungkan diagnosis |
| G5 | `posin-gate`/refinery tidak menjalankan tes Postgres (`TEST_DATABASE_URL` kosong) | Gate "hijau" tanpa query terbukti; berlaku untuk semua epic pilot |
| G6 | `gt mq integration status` membaca branch lokal usang di `.repo.git` ("Ahead of main: 0") | Laporan salah; `land` aman karena me-merge `origin/...` |
| G7 | Checkpoint otomatis membuat commit "WIP: checkpoint (auto)" di branch polecat dengan identitas git manusia, berisi gofmt di luar scope | Riwayat integration tercemar, atribusi salah |
| G8 | Bead polecat ditutup (`gt done`) sebelum MR merged | `closed_at` tidak bisa dipakai sebagai waktu merge |
| G9 | `gt dog done` gagal untuk dog: "gt done is for polecats only" | `hq-dog-alpha` tidak bisa idle |
| G10 | Cek kesiapan S1a memperingatkan `missing_acceptance_criteria` saat dispatch konsumen | Perlu dicek apakah AC di bead terbaca oleh cek kesiapan |
| G11 | `gt up` melaporkan daemon gagal karena daemon start sebelum Dolt siap, padahal daemon berjalan | Laporan start menyesatkan |

### 5.3 Agent dan lingkungan

- **agy:**
  - prompt izin berulang dan tidak bisa diizinkan per sesi;
  - `gt status` menampilkan `[claude]` untuk polecat agy;
  - kuota mingguan agy-a4 habis di tengah epic (bukan karena laptop tidur: habis pukul 00:01, laptop baru tidur 05:02), sehingga konsumen terhenti tanpa peringatan dini. Satu akun untuk 5 sesi paralel/berurutan menghabiskannya dalam ±1 jam.
  - Rinciannya ada di `gt-af2e`.
- **Spawn Claude gagal sementara (15:42–16:02):** gejalanya exit 1 < 250 ms untuk polecat maupun `hq-boot`, padahal `claude -p` normal setelahnya. Penyebabnya tidak terlacak, karena tmux menutup pane yang mati sehingga pesan errornya hilang.
- **Auto-approver prompt dan menjalankan `claude` dari sesi operator ditolak classifier.** Semua persetujuan prompt dilakukan manual.
- **Pilot tetap hidup saat laptop tidur.** Daemon hanya jalan saat DarkWake (heartbeat bolong 27–73 menit). Tidak ada kerusakan, tetapi pemantauan dan alarm stall tidak berarti selama itu; matikan pilot (`gt down`) sebelum laptop ditinggal.

## 6. Batasan

- AC8 diuji di pilot sendiri setelah epic selesai, bukan di town salinan.
- Token briefing tidak terukur.
- Klaim review leg r2 terhadap kode tidak diverifikasi secara independen; sintesis r2 mengumpulkan verdict dari layar tmux leg.
- Lead time terdistorsi oleh jeda semalam (menunggu keputusan + laptop tidur) dan gangguan spawn.
- Sampel satu epic. Perbandingan dengan workflow manual menunggu epic B.

## 7. Langkah berikutnya

1. Epic manual B oleh user → isi `../baseline/metrics-baseline.md` → keputusan gate Tahap 1.
2. Membuat bead untuk G1–G11 dan temuan proses 5.1 (2 dan 4); kandidat directives: aturan perubahan brief pasca-approval dan `affected` mencakup pemilik kontrak.
3. Epic S1c berikutnya: biarkan satu pertanyaan block terbuka untuk menguji block sempit.
