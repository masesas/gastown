# Re-analisis — Urutan Implementasi, Enhancement Ditunda, dan Pertanyaan Konfirmasi

Tanggal: 12 Oktober 2026. Legenda bukti: [V] terverifikasi, [U] laporan upstream, [I] inferensi, [A] asumsi. ID Rank dan P0.x mengacu ke `09`; ID D1–D8 mengacu ke `10`.

## 1. Urutan implementasi dan kriteria keberhasilan per tahap

Durasi tidak dijanjikan. Effort relatif ada di `09`. Sebuah tahap boleh dimulai hanya jika gate tahap sebelumnya lulus. Tahap yang gagal dikembalikan ke tahap atau keputusan asalnya, sesuai aturan "verify adalah loop".

| Tahap | Isi | Bisa paralel | Gate keluar (dapat diverifikasi) |
| --- | --- | --- | --- |
| **0. Fondasi pilot** | P0.1 toolchain (D2-A), P0.2 pilot town + backup (D1-B), P0.3 konfigurasi rig (D3, D5; akun Claude terdaftar; crew `contract-owner`), P0.4 baseline | P0.2 dan P0.3 setelah P0.1 | `gt daemon start` sukses di pilot town; satu bead BE dan satu bead mobile melewati sling → PR ke `epic/...`; checksum/`bd count` data per-repo tidak berubah; baseline tercatat; `go test ./...` hijau atau kegagalan terdokumentasi |
| **1. Kontrak task, state, dan protokol koordinasi** | Rank 1 (mode `warn`), Rank 5 (a)–(c), Rank 3P (overlay formula brief + owner, tanpa kode core) | Ketiganya paralel (file berbeda; 3P hanya TOML/directives) | Semua AC Rank 1 & 5 lulus; satu epic pilot BE + mobile dijalankan lewat brief yang direview dan disetujui Mayor; pertanyaan krusial terkumpul dengan tanda asumsi yang bisa ditelusuri; 0 dispatch konsumen sebelum skeleton merged; tidak ada false kill |
| **2. Gate evidence dan failover akun** | Rank 2: `observe` selama satu epic, lalu `enforce`; Rank 4a; quick wins refinery | Rank 2 dan 4a paralel (refinery/mq vs quota/daemon). Rank 4a di-merge setelah Rank 5 | Setelah `enforce`: 0 merge tanpa evidence dan tidak ada merge dengan evidence SHA berbeda. Rank 4a: dua polecat berjalan bersamaan dengan akun berbeda di atas satu `~/.claude`; polecat yang terkena limit dilanjutkan dengan akun lain (`--resume`) tanpa restart berulang dan tanpa menyentuh login claude-swap |
| **3. Penguatan koordinasi dan fallback provider** | Rank 3K (guard sling, feed fail-closed, `contract_ref`, `contract-stale`, guard kepemilikan `warn`, notifikasi gate); Rank 1 ke `enforce`; Rank 4b (bila Q10 tersedia) | 3K dan 4b paralel | Satu epic BE + mobile selesai dengan `contract_ref` terisi di semua bead konsumen; sling manual dengan blocker ditolak; 4b: satu polecat berhasil dilanjutkan di provider fallback dan lulus gate yang sama |
| **4. Enforce kepemilikan** | Guard kepemilikan Rank 3K(e) dari `warn` ke `enforce` | — | Laporan `warn` minimal satu epic menunjukkan presisi memadai (pelanggaran yang dilaporkan memang pelanggaran). Task resolusi konflik per MR tidak naik |
| **5. Optimasi** | Pilih dari daftar tunda (§ 2) berdasarkan bottleneck terukur | — | Setiap item punya pemicu terukur sebelum dikerjakan |

**Aturan lintas tahap:**

- Setiap patch fork adalah PR kecil dengan test. Patch formula ditaruh di overlay bila memungkinkan (D6).
- Setiap tahap diakhiri dengan pembaruan catatan kompatibilitas: commit fork, versi `bd`, Beads embed, Dolt, dan runtime agent (E20 minimal).
- Data existing per-repo tidak pernah dimigrasi tanpa dry-run dan uji restore pada salinan (D1).

## 2. Enhancement yang ditunda

| ID | Enhancement | Alasan ditunda | Pemicu untuk diprioritaskan |
| --- | --- | --- | --- |
| E02 | Kapasitas per akun/provider/resource class di scheduler | `max_polecats` global cukup untuk 2–4 polecat [V]; failover Rank 4 menangani akun yang limit | Rotasi Rank 4a sering terjadi (beban perlu dibagi proaktif), atau saturasi resource mesin (DB test, emulator) |
| E06 | Deteksi overlap otomatis dari label scope | Peta kepemilikan Rank 3 mencegah konflik di hot path (Revisi 1) | Task resolusi konflik masih material di path yang tidak ada di peta kepemilikan |
| E03 | Lease/fencing penuh | File lock per bead efektif di satu host [V]; cek kepemilikan minimum masuk Rank 5 | Multi-host/multi-town, atau force re-sling sering terjadi |
| E07 | Paket konteks berversi | Rank 1 menangani konteks per task; aturan repo sudah di `CLAUDE.md` | Agent melanggar aturan repo berulang meskipun prime lengkap |
| E08/E14 | Routing berbasis kapabilitas/evaluasi | Butuh data hasil per runtime | Setelah event `gate.result` terkumpul untuk ≥ 1–2 epic per runtime |
| E09 | Checkpoint recovery | Branch yang di-push dan bead notes sudah memberi recovery dasar | Recovery loss teramati (kerja hilang setelah crash) |
| E10 | Isolasi test lanjutan | Project BE sudah isolasi per port [V]; cukup konfigurasi P0.3 | Emulator/browser test paralel diperlukan |
| E11 | Review otomatis berbasis risiko | Bergantung pada mekanisme Rank 2 | Setelah Rank 2 `enforce`; jika review manusia menjadi bottleneck |
| E12 | Pipeline desain → verifikasi visual | Belum ada bukti kebutuhan mendesak; adapter image generation di luar scope | Epic UI besar dengan banyak rework visual |
| E13 | Dashboard | Kontrak event disiapkan (`10` § 3) | Event stabil selama satu epic dan ada pertanyaan operasional yang tidak terjawab via CLI |
| E15 (penuh) | Migrasi backlog existing | D1-B lebih aman untuk pilot | Pilot dinilai bernilai dan dua sistem tracking menjadi beban |
| E17 (penuh) | Compiler formula | Wiring validasi sudah masuk Rank 1 | Bug formula/dry-run mengganggu workflow Anda |
| E18 | Pesan durable | [U] #4634/#4711/#4607; dampak kecil pada 2–4 agent | Pesan/eskalasi hilang teramati, atau agent > 6 |
| E19 (penuh) | Retention & restore drill | Backup manual di P0.2 cukup untuk pilot | Sebelum D1-A (migrasi) atau saat town menjadi sumber utama |
| E21 | Batas tulis antar-workspace | Worktree terpisah + aturan repo; belum ada insiden | Ada perubahan di worktree lain atau main checkout |
| E22 | Beban control plane | Dashboard tidak dipakai sekarang | Proses `bd` menumpuk atau daemon lambat |
| E23 (lanjutan) | Kebijakan durable penuh | Mode `pr` di config rig bersifat durable [V]; risiko fallback hanya untuk strategi per-dispatch `local/direct` | Bila memakai strategi `local`/`direct` per convoy |
| E24 | Manifest artefak | Evidence Rank 2 mencakup kebutuhan minimum | Synthesis lintas-agent (desain, report) mulai rutin |
| E25 | Monorepo/sparse checkout | Repo Anda tidak cukup besar untuk membutuhkannya [A] | Waktu spawn worktree atau disk menjadi masalah |
| E26 | Observability aplikasi untuk agent | Log `ci-local` sudah menjadi evidence | Debugging integration test memakan banyak siklus agent |
| E27 | Event forge | Volume PR masuk rendah [A] | Banyak PR eksternal atau kebutuhan reaksi cepat terhadap review |
| E28 | Pause/drain durable | [U] #4475; dibutuhkan saat operasi maintenance | Sebelum migrasi data atau upgrade toolchain di town aktif |
| E29 (autodetect) | Autodetect stack | Konfigurasi manual dua repo lebih murah | Lebih dari ~5 repo dengan stack beragam |
| E30 | Identitas kanonik/split-brain | Satu town, satu socket | Lebih dari satu town atau mesin |

## 3. Metrik untuk membuktikan manfaat

Dikumpulkan sejak P0.4. Bandingkan antar-epic dengan kompleksitas sebanding (jumlah bead, repo yang terlibat). Laporkan median dan rentang. Jangan tampilkan persentase jika sampel kecil.

| Metrik | Definisi | Sumber data |
| --- | --- | --- |
| Verified throughput | Child bead "selesai" sesuai `10` § 2.6 per minggu | bd + event `merge.result`/`gate.result` |
| First-pass gate rate | MR yang lulus gate pada percobaan pertama ÷ MR yang masuk gate | `gate.result` |
| Rework loops | Jumlah FIX_NEEDED per bead selesai, dibedakan berdasarkan penyebab (salah tafsir AC, regresi, konflik) | bd notes/label |
| Intervensi manusia | Approval, eskalasi, override, dan perbaikan manual per bead selesai | event `escalation` + override |
| False kill | Sesi yang dibunuh padahal kemudian terbukti sehat | `session.lifecycle` |
| Konflik | Task "Resolve merge conflicts" per MR merged | bd |
| Biaya koordinasi | Durasi dan token briefing per epic; jumlah `contract-change`; waktu tunggu konsumen terhadap skeleton | bd + event + log sesi |
| Pertanyaan & asumsi | Pertanyaan krusial per epic; % jawaban berbeda dari asumsi; bead rework akibat asumsi; waktu tunggu `block` | bead `gt:question` + event `question.*` |
| Dampak limit akun | Waktu idle karena limit; jumlah rotasi; restart sia-sia; first-pass gate rate per provider | `session.lifecycle` + `gate.result` |
| Lead time & queue time | Ready → merged; dirinci ke tunggu blocker, kapasitas, gate, dan review | event + timestamp bd |
| Escaped defects | Bug di epic/master yang ditautkan ke MR yang lolos gate | bd (link `discovered-from`) |

## 4. Pertanyaan konfirmasi

Dikelompokkan berdasarkan dampak. Setiap pertanyaan punya default yang dipakai dokumen ini bila belum dijawab.

**Status jawaban (Revisi 1, 12 Oktober 2026):**

| Q | Jawaban |
| --- | --- |
| Q1 | Ya (D1-B) |
| Q2, Q5, Q6, Q7 | Tidak ada koreksi; default berlaku |
| Q3 | Naikkan Beads di fork (D2-A) |
| Q4 | `make ci-local` ±4 menit → gate satu tingkat |
| Q8 | Dilampaui keputusan menjadikan E01 prioritas |
| Q9 | Boleh; repo sudah diindeks |
| Q11 | Mayor menyetujui brief; pertanyaan krusial dikumpulkan untuk Anda; pekerjaan berbasis asumsi diberi tanda dan dikerjakan ulang bila jawaban berbeda (`14`) |
| Q10 | Login berlangganan → 4b `home-per-account` + spike wajib |
| Q12 | Tidak butuh connectors/Remote Control → 4a opsi A (token per proses) |

Teks pertanyaan di bawah dipertahankan sebagai catatan.

### A. Data dan toolchain (memengaruhi Tahap 0; mahal dibalik)

- **Q1 — Strategi data (D1).** Setuju pilot memakai DB town baru untuk epic baru, sementara backlog existing tetap di bd per-repo?
  - Default: **ya (D1-B)**.
  - Jika Anda menginginkan migrasi penuh sejak awal, P0.2 naik menjadi M–L dan butuh spike impor.
- **Q3 — Versi bd (D2).** Apakah ada tooling, hook, atau skill lain yang mewajibkan `bd 1.3.0`?
  - Default: **naikkan Beads di fork (spike A)**, dengan fallback town ber-`bd` terisolasi (C).
  - Jika `bd` harus tetap 1.3.0 secara global, opsi B gugur.

### B. Alur integrasi (memengaruhi Rank 2 dan beban supervisi)

- **Q2 — Approval (D3).** Selama pilot, approval manusia di setiap PR child, atau hanya di PR epic → master?
  - Default: **setiap child selama pilot**.
  - Pilihan "hanya epic" mewajibkan Rank 2 `enforce` lebih dulu.
- **Q4 — Perintah gate.** Gate BE `make ci-local` (berapa lama sekali jalan?) atau subset Gradle untuk per-MR, dengan `ci-local` di level wave/epic? Gate mobile `fvm flutter analyze && fvm flutter test` + `build_runner`?
  - Default: **BE `make ci-local`, mobile analyze + test**.
  - Jika `ci-local` > ~15 menit, perlu dua tingkat gate, dan ini memengaruhi desain Rank 2.
- **Q6 — Penamaan branch (D5).** Setuju branch polecat memakai prefix `<type>/gt-<bead>` (bukan `ai/*`) dan integration branch `epic/{title}`?
  - Default: **ya**.

### C. Kontrak dan arah fork

- **Q5 — Format kontrak (D8).** Artefak kontrak apa yang dihasilkan BE saat ini (OpenAPI/springdoc, dokumen di `docs/spec`, atau lainnya)?
  - Default: **dokumen/berkas yang sudah ada, di-pin path@sha**.
- **Q7 — Mode fork (D6).** Fork produk murni, atau ada niat berkontribusi ke upstream?
  - Default: **fork produk, patch tetap kecil dan bisa di-PR**.
- **Q8 — Kuota.** Seberapa sering limit akun Claude tercapai pada workflow Anda saat ini?
  - **Dilampaui oleh keputusan Revisi 1:** E01 dijadikan prioritas (Rank 4). Frekuensi limit tetap diukur sebagai metrik Rank 4.

### D. Tooling analisis

- **Q9 — Index graph.** Izinkan mengindeks repo gastown ke `codebase-memory-mcp`?
  - Default: **belum diindeks**. Analisis ini berbasis grep + Read dan diberi label "graph unavailable" di `08` § 1.3.
  - Indeks dibutuhkan untuk impact analysis yang lebih andal saat implementasi.

Bagian yang **bersyarat** pada jawaban:

| Bagian | Bergantung pada |
| --- | --- |
| P0.1, P0.2 | Q1, Q3 |
| Desain gate dua tingkat di Rank 2 | Q4 (terjawab: tidak diperlukan) |
| Format `contract_ref` di Rank 3 | Q5 |
| Scope Rank 4b | Q10 |
| Opsi A vs C untuk Rank 4a | Q12 |
| Pemberi approval brief Rank 3 | Q11 (terjawab: Mayor + protokol pertanyaan `14`) |

Bagian lain tetap berlaku apa pun jawabannya.
