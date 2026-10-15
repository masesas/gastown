# Re-analisis — Prasyarat, Ranking Kandidat, dan Detail Top 5

Tanggal: 12 Oktober 2026. Legenda bukti: [V] terverifikasi, [U] laporan upstream, [I] inferensi, [A] asumsi. Matriks bukti lengkap ada di `08` § 2.

## 1. Cara menilai

Setiap kandidat dinilai dengan tujuh kriteria:

1. Dampak pada correctness hasil.
2. Dampak pada produktivitas multi-agent.
3. Pengurangan rework, konflik, dan supervisi.
4. Kemampuan lain yang terbuka setelah kandidat dikerjakan.
5. Effort dan beban maintenance. Bobot kriteria ini tinggi karena upstream tidak aktif, sehingga semua patch menjadi tanggungan fork.
6. Risiko dan kemudahan validasi.
7. Kekuatan bukti gap pada versi yang dipakai.

Skala yang dipakai di tabel: **T** = tinggi, **S** = sedang, **R** = rendah.

Effort dinyatakan relatif:

| Ukuran | Kira-kira | Ruang lingkup |
| --- | --- | --- |
| **XS** | < 1 hari | Konfigurasi |
| **S** | 1–3 hari ideal | Perubahan terlokalisasi + test |
| **M** | 3–8 hari | Beberapa paket + perubahan formula |
| **L** | > 8 hari | Desain baru |

Asumsi effort: satu engineer yang sudah familiar dengan Go dan dibantu agent, termasuk test unit/integrasi, tetapi tanpa waktu review/menunggu. Ini bukan komitmen jadwal.

## 2. Prasyarat Tahap 0 (bukan enhancement, tetapi wajib lebih dulu)

Prasyarat dipisahkan dari enhancement. Tanpa ini, pilot tidak bisa berjalan atau hasilnya tidak dapat dipercaya.

| ID | Prasyarat | Isi minimum | Asal | Effort |
| --- | --- | --- | --- | --- |
| P0.1 | Penyelarasan toolchain | Lihat P0.1 di bawah | E20 minimal | S–L, ditentukan spike |
| P0.2 | Pilot town terisolasi + backup data | Lihat P0.2 di bawah | E15/E19 minimal | S |
| P0.3 | Konfigurasi rig untuk stack Anda | Lihat P0.3 di bawah | E29 via config, E23 minimal | XS–S |
| P0.4 | Baseline pengukuran | Lihat P0.4 di bawah | Bagian minimum E13 | S |

**P0.1 — Penyelarasan toolchain.**
- Build `gt` dari fork dengan flag ICU (`make build`).
- Penyelarasan Beads: **naikkan Beads di fork** (D2-A, keputusan Anda di Revisi 1).
- Spike 1–2 hari: naikkan `github.com/steveyegge/beads` ke versi yang cocok dengan `bd` 1.3.0, lalu jalankan `go test ./...`. Bila lebih dari 2 hari, jalankan pilot dengan opsi C (bd terisolasi) sambil spike dilanjutkan.
- Selidiki 2 test `internal/beads` yang gagal lokal.
- Verifikasi bahwa `gt daemon start` pada salinan data tidak terblokir (`daemon.go:1197`).

**P0.2 — Pilot town terisolasi + backup data.**
- Buat town di direktori baru. Jangan pakai main checkout atau `.beads` existing.
- Backup kedua DB per-repo (`dolt dump`/salinan direktori) sebelum operasi apa pun.
- Rig pilot memakai DB baru untuk epic baru (keputusan D1). Data existing tidak dipindahkan.
- Matikan atau tinjau patrol destruktif yang dilaporkan upstream sebelum menyalakan daemon: auto-close stale (#4911/#4885) dan `wisp gc --age 1h --force` (#4830). Ini [U], belum diverifikasi di config.

**P0.3 — Konfigurasi rig untuk stack Anda.** Isi `settings/config.json` → `merge_queue`:
- **BE:** `test_command` → `make ci-local` (±4 menit per run, jawaban Q4). Satu tingkat gate per MR sudah cukup.
- **Mobile:** `fvm flutter analyze`, `fvm flutter test`, dan `build_runner` bila perlu.
- `merge_strategy=pr`.
- `integration_branch_template=epic/{title}`.
- `polecat_branch_template` diselaraskan dengan konvensi branch.
- `RoleAgents` diset ke Claude untuk semua role. Daftarkan akun Claude yang tersedia dengan `gt account add` agar Rank 4 bisa langsung diuji.
- Mapping `GT_POLECAT` → `CI_PG_PORT` lewat `setup_command` atau wrapper.
- Buat crew `contract-owner` di masing-masing rig (persiapan Rank 3).
- `quota_dog` tetap **off** sampai Rank 4a selesai. Rotasi bawaannya menyalin token antar-keychain (`12` § 3.1).

**P0.4 — Baseline pengukuran.**
- Jalankan satu epic kecil dengan workflow manual existing dan satu dengan Gas Town.
- Catat metrik (`11` § 3) secara manual atau dari `.events.jsonl` + bd.

**Gate keluar Tahap 0:**
- `gt` berjalan di pilot town.
- Satu bead BE dan satu bead mobile berhasil melewati jalur sling → polecat → refinery `pr` → PR ke `epic/...`.
- Data per-repo existing tidak tersentuh, dibuktikan dengan checksum atau `bd count` sebelum dan sesudah.
- Baseline tercatat.

## 3. Tabel perbandingan kandidat

Kandidat diambil dari E01–E30 ditambah satu kandidat baru (E31). Hanya kandidat yang realistis untuk tahap awal yang dicantumkan.

| Kandidat | Correctness | Produktivitas multi-agent | Kurangi rework/supervisi | Unlock | Effort | Bukti gap | Keputusan |
| --- | --- | --- | --- | --- | --- | --- | --- |
| **E04** Kontrak task utuh + readiness gate | T | S | T | T (E05, E11, E31) | S | [V] kuat | **Rank 1** |
| **E05** Gate merge mekanis + evidence SHA | T | S | T | T (E11, E13, E14) | M | [V] kuat | **Rank 2** |
| **E31+ (baru, Revisi 1)** Koordinasi contract-first: brief + owner + guard dependensi + pin kontrak | T (BE↔mobile, shared) | T | T | S (E06, E24) | M | [V] sling tanpa cek dep; feed fail-open; gate manusia tidak dikelola; primitif convoy/wave ada | **Rank 3** |
| **E01 slice (Revisi 1–2)** Akun Claude bersamaan dengan satu config global (4a) + fallback provider (4b) | R | T saat limit tercapai | S | S (E02, E08) | M (4a) + M–L (4b) | [V] rotasi via salin token ke keychain default; `account add` membuat config dir hampir kosong; limited tidak disimpan | **Rank 4**, prioritas keputusan Anda |
| **E16** State fail-closed di boundary lifecycle | T | T | T | S (E03, E09) | M | [V] kode + [U] #4890/#4632 | **Rank 5** |
| E06 Deteksi overlap otomatis dari label scope | S | T | T (bila konflik sering) | S | M | [V] gap ada; [A] frekuensi konflik belum diukur | Ditunda; peta kepemilikan Rank 3 menggantikan peran utamanya |
| E17 Validasi formula | S | R | S | R | XS–S (wiring) | [V] | Quick win, bukan top 5 |
| E11 Review berbasis risiko | S | R | S | R | S setelah Rank 2 | [V] tak terhubung | Tahap 3, di atas mekanisme Rank 2 |
| E03 Lease/fencing penuh | S | S | S | S | M–L | [V] tak ada; risiko kecil di satu host | Ditunda; cek kepemilikan minimum masuk Rank 3 |
| E02 Kapasitas per akun/resource di scheduler | R | S | S | S | M–L | [V] scheduler global sudah ada | Ditunda; pakai `max_polecats` + failover Rank 4 |
| E18 Pesan durable | S | S | S | R | M | [U] | Ditunda; pemicu: pesan hilang teramati |
| E20 Kompatibilitas versi | T | — | — | Prasyarat | S–L | [V] skew lokal | **P0.1** |
| E15/E19 Adopsi data + backup | T (data) | — | — | Prasyarat | S | [V] | **P0.2** |
| E29 Profil stack | S | S | S | — | XS via config | [V] config ada | **P0.3** (tanpa autodetect) |
| E10 Isolasi tes | S | S | S | — | XS via config | [V] project sudah isolasi | **P0.3** |
| E21, E22, E28, E30, E23 lanjutan | S | R pada skala kecil | R | R | M | [U] | Ditunda (`11` § 2) |
| E07, E08, E09, E12, E13, E14, E24–E27 | bervariasi | bervariasi | bervariasi | — | M–L | campuran | Ditunda (`11` § 2) |

### Alasan ranking

1. **Rank 1 (E04).** Effort paling kecil dan bukti paling kuat. Rank ini langsung menyentuh akurasi terhadap requirement, yang merupakan tujuan utama. Data AC sudah ada di bd berkat policy Anda, tetapi saat ini hilang di jalan menuju worker. Rank 2 dan Rank 3 juga membutuhkan kontrak yang terbaca mesin.
2. **Rank 2 (E05).** Nilai correctness-nya paling besar karena mencegah "selesai palsu" masuk ke branch epic. Rank ini di bawah Rank 1 karena lebih mahal, dan karena evidence yang bermakna butuh AC yang jelas.
3. **Rank 3 (E31+ koordinasi contract-first).** Menggabungkan usulan Anda (brief, contract owner, konsumen terblokir, finalisasi) dengan guard dependensi dan pin kontrak dari E31 versi awal. Ini menyelesaikan dua kebutuhan eksplisit sekaligus: BE ↔ mobile dan konflik di area bersama. Sebagian besar bisa dijalankan dulu dengan primitif yang ada (formula, convoy stage, bead `gate`, crew). Kode baru hanya untuk celah yang membuat kesepakatan bisa dilanggar diam-diam.
4. **Rank 4 (E01 slice).** Dinaikkan atas keputusan Anda, bukan atas data baseline. Alasan teknis yang mendukung: mesin rotasinya sudah ada, sehingga slice 4a sebagian besar berupa wiring + perbaikan. Selain itu, cara rotasi saat ini (salin token antar-keychain) sebaiknya tidak dinyalakan apa adanya. Bila tidak ditangani, satu-satunya pilihan saat limit tercapai adalah menunggu.
5. **Rank 5 (E16 slice).** Gap ini merusak produktivitas paralel: worker sehat dibunuh, dan lookup yang gagal diperlakukan sebagai "tidak ada pekerjaan". Kerusakannya membesar seiring jumlah polecat. Rank ini di posisi terakhir karena gejalanya [U] dan belum terlihat di mesin Anda. Namun effort-nya terlokalisasi, dan bisa dikerjakan paralel sejak Tahap 1.

**Catatan urutan.** Rank bukan urutan pengerjaan. Urutan per tahap ada di `11` § 1: Rank 1 dan 5 di Tahap 1, Rank 2 dan 4a di Tahap 2, Rank 3 (kode) dan 4b di Tahap 3. Rank 3 versi prosedural (tanpa kode baru) sudah berjalan sejak pilot Tahap 1.

**Bukan top 5 meski bernilai:**

- **E06 deteksi overlap otomatis.** Peta kepemilikan di Rank 3 mencegah konflik pada hot path dengan cara yang lebih langsung. Deteksi otomatis baru layak bila konflik masih terjadi di luar hot path yang terdaftar.
- **E02 kapasitas per akun di scheduler.** Failover Rank 4 menangani akun yang terkena limit. Kapasitas per akun baru diperlukan bila beban harus dibagi secara proaktif.

---

## 4. Detail enhancement terpilih

### Rank 1 — Kontrak task utuh sampai ke worker + readiness gate

1. **Nama/ID:** E04 (kontrak task terstruktur), ditambah wiring validasi variabel formula dari E17.
2. **Masalah dan skenario gagal.** Contoh: bead `feat: validasi benturan periode reservasi` punya 12 baris deskripsi, AC berisi 4 checklist (termasuk "batas akhir eksklusif"), dan `design` berisi keputusan zona waktu. Polecat menjalankan `gt prime` dan hanya melihat 5 baris pertama. Formula memang menyuruh `bd show`, tetapi itu bergantung pada kepatuhan LLM. Akibatnya implementasi memakai batas inklusif, test lulus, dan kesalahan baru ditemukan saat review manusia. Hasilnya rework.
3. **Bukti.**
   - [V] `internal/cmd/prime.go:960-977` (`maxLines := 5`).
   - [V] Tidak ada pembaca `.Design` atau `.Notes` di jalur prime.
   - [V] `HasUncheckedCriteria` hanya dipakai di done/merge (`beads/beads.go:306-318`), dan bead tanpa AC lolos.
   - [V] Sling guards hanya memeriksa status (`cmd/sling.go:616-660`).
   - [V] `ValidateTemplateVariables` hanya dipanggil dari test.
4. **Kondisi saat ini dan gap.**
   - Ada: data AC di bd; instruksi formula "Scope is a contract".
   - Gap: render penuh di prime, readiness check, dan validasi variabel saat sling.
5. **Alasan diprioritaskan.** Rasio manfaat terhadap effort tertinggi, dan menjadi prasyarat makna evidence di Rank 2.
6. **Dampak ke workflow Anda.**
   - Bead yang ditulis sesuai `bd-issue-policy` benar-benar sampai utuh ke agent.
   - Bead yang belum siap ditolak sebelum menghabiskan sesi agent.
7. **Scope minimum.**
   - (a) `gt prime` merender deskripsi penuh, `acceptance_criteria`, `design`, dan `notes`, dengan batas ukuran yang bisa dikonfigurasi dan penanda terpotong yang eksplisit.
   - (b) Readiness check di sling dan dispatch scheduler, untuk tipe `task`/`feature`/`bug`. Wajib ada deskripsi dan ≥1 item AC. Bisa di-override dengan `--force-unready`, dan alasan override dicatat.
   - (c) Panggil `ValidateTemplateVariables` sebelum `bd cook`. Gagal berarti tolak.
   - (d) Pengaturan per rig: `readiness: off|warn|enforce`, default `warn`.
8. **Di luar scope:** schema task baru; compiler formula penuh; penilaian kualitas isi AC oleh LLM.
9. **Dependensi dan urutan:** setelah P0.1. Tidak bergantung pada enhancement lain. Kerjakan pertama.
10. **Effort:** S. Asumsi: perubahan di `prime.go`, `sling.go`, `capacity_dispatch.go`, dan konfigurasi rig. Keyakinan: sedang-tinggi.
11. **Trade-off, risiko, rollout/rollback.**
    - Konteks prime bertambah panjang (biaya token).
    - Mode `enforce` bisa memblokir bead lama. Karena itu rollout dimulai di `warn`, lalu naik ke `enforce` per rig.
    - Rollback: set `readiness: off`. Perubahan render bersifat aditif.
12. **Acceptance criteria.**
    - [ ] Bead dengan deskripsi 30 baris dan 4 AC tampil penuh di output `gt prime`. Diuji dengan test golden output.
    - [ ] Sling bead `feature` tanpa AC ditolak dengan pesan alasan dan exit nonzero pada mode `enforce`. Pada mode `warn`, bead tetap di-dispatch dengan peringatan dan event tercatat.
    - [ ] Formula dengan `{{var}}` yang tidak didefinisikan ditolak sebelum `bd cook` dijalankan.
    - [ ] `--force-unready` mencatat actor dan alasan pada bead (notes/event).
13. **Metrik dan cara membuktikan.**
    - Rasio FIX_NEEDED atau request-changes yang disebabkan "salah tafsir requirement", berdasarkan label penyebab saat review.
    - Jumlah bead yang ditolak readiness per minggu.
    - Bandingkan dengan baseline P0.4 pada epic dengan kompleksitas sebanding.

### Rank 2 — Gate merge mekanis berbasis evidence terikat SHA

1. **Nama/ID:** E05 (kelulusan berbasis bukti). Menyentuh E23 hanya pada aspek "policy gate tidak dilewati diam-diam".
2. **Masalah dan skenario gagal.**
   - Skenario 1: refinery (LLM) menjalankan test pada merge rehearsal, lalu 2 test gagal. Agent menilai kegagalan itu "pre-existing", membuat bug, dan tetap merge (`formula.toml:583-592`). Epic menerima regresi.
   - Skenario 2: polecat mengklaim `--pre-verified` tanpa bukti. Tidak ada mekanisme yang mengecek.
   - Skenario 3: pada mode `pr` untuk `oto-rent-be` yang Actions-nya mati, `gh pr checks --watch` tidak punya check untuk ditunggu [I]. Setelah approval manusia, PR di-merge tanpa `local-ci` pada SHA final.
3. **Bukti.**
   - [V] Engine Go dengan gates tidak terhubung.
   - [V] Gate produksi berbentuk prosa formula (`mol-refinery-patrol.formula.toml:424-466`).
   - [V] MR hanya menyimpan `commit_sha` dan `pre_verified` hasil klaim (`cmd/done.go:1744-1751`).
   - [V] `gt mq post-merge` hanya memeriksa ancestry (`cmd/mq.go:601-619`).
   - [V] Status `local-ci` per SHA sudah ada di BE (`scripts/ci-local.sh`).
4. **Kondisi saat ini dan gap.**
   - Ada: perintah gate yang bisa dikonfigurasi, mode `pr`, status `local-ci` di project, dan kode gate Go yang belum terhubung.
   - Gap: (a) verifier deterministik yang memutuskan boleh atau tidak merge; (b) catatan evidence terikat SHA pada MR; (c) larangan bypass "pre-existing" tanpa persetujuan eksplisit.
5. **Alasan diprioritaskan.** Rank ini paling langsung mengubah "task ditutup" menjadi "pekerjaan valid".
6. **Dampak ke workflow Anda.**
   - Mekanisme `local-ci` yang sudah Anda bangun menjadi syarat merge yang ditegakkan program, bukan instruksi untuk agent.
   - Review manusia di PR bisa fokus pada desain dan perilaku, bukan memeriksa apakah test sudah dijalankan.
7. **Scope minimum.**
   - (a) Perintah baru `gt mq verify <mr> --sha <sha>`. Perintah ini memeriksa evidence dari sumber yang dikonfigurasi per rig, dengan dua mode:
     - `commit_status:<context>`: membaca status GitHub, mis. `local-ci`, pada SHA head PR atau merge rehearsal.
     - `run`: menjalankan gate yang dikonfigurasi dan menulis hasilnya.
   - (b) Hasil verify ditulis ke bead MR: `{sha, base_sha, source, command, exit, duration, log_ref, verified_at}`.
   - (c) Langkah merge di formula refinery wajib memanggil `gt mq verify`. Exit nonzero berarti tidak merge. Jalur "pre-existing" hanya boleh lewat label override yang diberikan manusia.
   - (d) Perbaiki inkonsistensi `require_review` vs syarat `APPROVED`.
   - (e) Kesegaran SHA: PR harus up to date dengan `epic/<nama>` sebelum verify. Bila epic sudah maju, polecat atau refinery me-rebase, lalu `make ci-local` dijalankan ulang pada SHA baru. Dengan durasi ±4 menit (Q4), biaya ulang ini dapat diterima, jadi tidak perlu gate dua tingkat.
8. **Di luar scope:**
   - Menghubungkan seluruh engine Go refinery.
   - Invalidasi evidence otomatis berbasis analisis dampak.
   - Verifikasi visual (E12).
   - Reviewer AI otomatis (E11, dibangun di atas ini nanti).
9. **Dependensi dan urutan.** Setelah P0.3 (konfigurasi gate) dan idealnya setelah Rank 1, karena AC dirujuk dalam evidence. Bisa paralel dengan Rank 4a (file berbeda).
10. **Effort:** M. Asumsi: verifier Go kecil, patch formula refinery, dan field MR baru via description fields yang sudah ada (`beads/fields.go`). Keyakinan: sedang-rendah, karena perilaku mode `pr` tanpa Actions belum diuji.
11. **Trade-off, risiko, rollout/rollback.**
    - Gate yang lebih ketat meningkatkan waktu antre merge. Flaky test akan memblokir merge, sehingga perlu kebijakan retry 1× yang tercatat.
    - Batas atas throughput merge serial kira-kira 60 ÷ (4 menit gate + waktu rebase) MR per jam per rig [I]. Untuk 2–4 polecat ini bukan bottleneck. Ukur di pilot.
    - Rollout: mode `observe` dulu, yang mencatat evidence tanpa memblokir, selama satu epic. Setelah itu naik ke `enforce`.
    - Rollback: konfigurasi `verify: off`. Formula lama tetap tersedia lewat overlay.
12. **Acceptance criteria.**
    - [ ] MR dengan status `local-ci=failure` atau tanpa status pada SHA yang akan di-merge tidak di-merge. Refinery mengirim FIX_NEEDED dengan referensi log.
    - [ ] Evidence untuk SHA A ditolak saat SHA head PR adalah B.
    - [ ] Override "pre-existing" hanya diterima jika label override diberikan oleh actor manusia, dan override itu tercatat.
    - [ ] Bead MR yang merged selalu memiliki record evidence lengkap. Diuji lewat query sampel.
13. **Metrik.**
    - Escaped defect: bug di epic/master yang ditautkan ke MR yang lolos gate.
    - Jumlah merge tanpa evidence, dengan target 0 setelah `enforce`.
    - Waktu antre merge, sebelum dan sesudah.

### Rank 3 — Koordinasi contract-first: brief, contract owner, dan guard dependensi

1. **Nama/ID:** **E31+ (Revisi 1)**. Menggabungkan usulan briefing Anda dengan E31 versi awal (guard dependensi + pin kontrak). Mengambil peran utama E06 lewat peta kepemilikan hot path. Evaluasi desain dan protokol lengkapnya ada di `12` § 2.
2. **Masalah dan skenario gagal.**
   - Skenario 1 (lintas-rig): epic "Tarif host" punya bead BE `kontrak API tarif` yang memblokir bead mobile `form tarif`. Mayor menjalankan `gt sling` manual untuk bead mobile. Sling tidak memeriksa dependensi, sehingga mobile dikerjakan dengan asumsi DTO lama. Alternatifnya, lookup Dolt gagal, feed convoy fail-open, dan bead mobile ikut dirilis.
   - Skenario 2 (satu rig): dua polecat BE paralel sama-sama menambah method ke service di `shared/`, dan masing-masing membuat DTO yang mirip. Refinery menemukan konflik di akhir, atau lebih buruk: tidak ada konflik teks tetapi ada dua DTO duplikat.
   - Skenario 3 (kontrak berubah): `price` diganti `priceAmount` setelah mobile selesai, tanpa sinyal ke bead mobile.
3. **Bukti.**
   - [V] Sling manual tanpa cek dependensi (`cmd/sling.go:616-660`; grep sling*.go hanya menemukan cek status).
   - [V] Feed convoy fail-open saat store/lookup error (`convoy/operations.go:202-204`, `:215-218`). Scheduler fail-closed (`cmd/capacity_dispatch.go:922-927`).
   - [V] Bead `gate` ada (`constants/constants.go:183-185`), tetapi gate manusia tidak dikelola patrol (`mol-deacon-patrol.formula.toml:345-371`) dan tidak ada notifikasi.
   - [V] `gt convoy stage` menghitung wave dan menahan dependen dari bead `gate`/`decision` yang terbuka (`cmd/convoy_stage.go:990-1092`).
   - [V] Role kustom ditolak (`config/roles.go:143-145`); crew bernama + `--crew` tersedia (`cmd/sling.go:306-308`).
   - [V] Tidak ada konsep artefak kontrak, peta kepemilikan, atau cek diff terhadap kepemilikan.
4. **Kondisi saat ini dan gap.**
   - Ada & jalan: formula review paralel + sintesis, convoy stage/launch berbasis wave, dependensi `blocks`, crew bernama, mail thread/channel.
   - Ada, belum terhubung: `TriggerSynthesisIfReady` tanpa caller; formula `mol-validate-prd` dirujuk tetapi tidak ada.
   - Gap: (a) sling manual dan feed bisa melewati blocker; (b) gate approval tanpa pemilik dan notifikasi; (c) kontrak tidak di-pin ke bead konsumen; (d) kesepakatan kepemilikan tidak ditegakkan saat `gt done`.
5. **Alasan diprioritaskan.** Dua kebutuhan eksplisit Anda (BE ↔ mobile dan pencegahan konflik area bersama) diselesaikan oleh satu mekanisme. Bagian proseduralnya bisa dipakai sejak pilot tanpa menunggu kode.
6. **Dampak ke workflow Anda.**
   - Area bersama (`shared/`, migrasi, DTO, komponen UI bersama, design token) hanya ditulis oleh satu owner per rig.
   - Konsumen mulai setelah skeleton kontrak merged, dan tahu versi kontrak yang dipakai.
   - Brief disetujui Mayor (Revisi 3). Keterlibatan Anda berupa daftar pertanyaan krusial yang dijawab sekaligus. Pekerjaan berbasis asumsi diberi tanda dan dikerjakan ulang hanya bila jawaban berbeda (`14`).
7. **Scope minimum.** Dikirim dalam dua bagian.
   - **3P — Prosedural (tanpa kode core, Tahap 1).**
     - Overlay formula `mol-contract-brief`: susun brief → leg review per area konsumen (maks 2 ronde) → cek konsistensi mekanis → Mayor menutup bead `gate` approval. Brief tanpa review tidak boleh disetujui.
     - Directives Mayor untuk protokol pertanyaan (`14` § 2): klasifikasi krusial `proceed`/`block`, bead pertanyaan `gt:question`, label `assumes:Q-*` (dipropagasi manual ke task dependen), daftar pertanyaan via `gt mail send --human`, dan rework berdasarkan tanda.
     - Overlay formula `mol-contract-work` untuk crew `contract-owner`: skeleton kontrak (memblokir) dipisah dari implementasi shared (tidak memblokir kecuali dinyatakan).
     - Dispatch hanya lewat `gt convoy stage` + `launch` atau scheduler. **Dilarang** memakai `gt sling` manual untuk bead konsumen sampai 3K(a) selesai (aturan operasional di directives Mayor).
   - **3K — Kode fork (Tahap 3).**
     - (a) Sling manual menolak bead dengan blocker terbuka atau status `unknown`, kecuali `--ignore-deps`. Override tercatat.
     - (b) Feed convoy fail-closed saat store/lookup gagal.
     - (c) Field `contract_ref = <repo>:<path>@<sha>` diisi saat skeleton merged. Bead konsumen menyalinnya saat dispatch dan prime merendernya (memakai Rank 1).
     - (d) Bead kontrak dibuka ulang atau `contract_ref` berubah → bead konsumen yang masih terbuka mendapat label `contract-stale`, dan Mayor menerima mail.
     - (e) Peta kepemilikan (`owns: <path-glob>` pada bead owner). Saat `gt done`, diff konsumen yang menyentuh path milik owner dilaporkan. Mode `warn` lalu `enforce`.
     - (f) Bead pertanyaan `block` yang terbuka lebih dari N jam memicu pengingat ke Anda.
     - (g) Propagasi otomatis label `assumes:*` dari blocker yang selesai ke task dependen saat dispatch, dan pembaruan field `affected` di bead pertanyaan.
     - (h) PR epic → master ditolak selama ada child dengan `assumes:*` yang belum terjawab atau bead rework yang belum merged.
8. **Di luar scope:**
   - Diskusi bebas multi-agent dan protokol konsensus (`12` § 2.2).
   - Role `contract-owner` baru di core.
   - Generator OpenAPI/client, diff semantik kontrak, dan atomic merge lintas repo.
   - Deteksi overlap otomatis untuk path di luar peta kepemilikan (E06).
9. **Dependensi dan urutan.** 3P setelah P0.3 (crew dibuat). 3K(c) setelah Rank 1, karena memakai render prime. Format `contract_ref` mengikuti default Q5 (artefak yang sudah ada, di-pin path@sha).
10. **Effort:** 3P **S** (TOML + directives), 3K **M** (termasuk (g)–(h), masing-masing kecil). Keyakinan: sedang untuk 3K(a)–(d), rendah untuk (e) karena kualitas peta kepemilikan bergantung pada disiplin penyusun brief. Asumsi: `bd blocked`/`bd show` me-resolve dependensi lintas-rig dengan benar, dan `workflow_target` diteruskan lewat feed daemon ([I], diuji di pilot).
11. **Trade-off, risiko, rollout/rollback.**
    - Owner menjadi titik serial. Mitigasi: owner hanya mengerjakan skeleton yang memblokir; implementasi shared berjalan paralel dengan konsumen.
    - Briefing menambah latensi dan biaya token. Hanya wajib untuk epic yang menyentuh hot path, kontrak lintas-rig, atau ≥ 3 bead paralel di satu rig.
    - Rollout: 3P di epic pilot pertama; 3K(a)/(b) sebelum ada dispatch manual; (e) `warn` minimal satu epic.
    - Rollback: overlay formula bisa dilepas; flag `deps_guard: warn` dan `ownership_guard: off`.
12. **Acceptance criteria.**
    - [ ] Epic pilot BE + mobile dijalankan dengan 3P: brief direview dan disetujui Mayor lewat penutupan bead gate; tidak ada bead konsumen yang di-dispatch sebelum skeleton merged.
    - [ ] Pertanyaan krusial terkumpul sebagai bead `gt:question`. Task terkait bertanda `assumes:Q-*`, termasuk task dependen. Daftar dikirim ke Anda paling lambat sebelum PR epic → master; pertanyaan `block` dikirim segera.
    - [ ] Jawaban yang sama dengan asumsi menghapus tanda tanpa rework. Jawaban berbeda menghasilkan bead rework hanya untuk task bertanda, dengan urutan kontrak lebih dulu.
    - [ ] PR epic → master ditolak selama ada tanda asumsi terbuka.
    - [ ] `gt sling <bead-konsumen>` dengan blocker terbuka ditolak. Dengan `--ignore-deps`, sling diterima dan override tercatat.
    - [ ] Lookup dependensi error → feed tidak me-release bead.
    - [ ] Bead konsumen menampilkan `contract_ref` di prime.
    - [ ] Bead kontrak dibuka ulang → bead konsumen terbuka mendapat label `contract-stale` dan Mayor menerima mail.
    - [ ] Diff konsumen yang menyentuh path milik owner muncul sebagai peringatan di MR (`warn`), dan ditolak di `enforce`.
13. **Metrik.**
    - Task resolusi konflik per MR merged, dan DTO/service duplikat yang ditemukan saat review.
    - Pertanyaan krusial per epic; proporsi jawaban yang berbeda dari asumsi; jumlah bead rework akibat asumsi; waktu tunggu pertanyaan `block`.
    - Rework konsumen karena perubahan kontrak.
    - Jumlah bead `contract-change` per epic.
    - Waktu tunggu konsumen terhadap skeleton (mendeteksi owner sebagai bottleneck).
    - Biaya token dan durasi briefing per epic.

### Rank 4 — Failover akun Claude dan fallback provider (E01 slice)

1. **Nama/ID:** E01, dipersempit menjadi dua slice yang dikirim terpisah: **4a** akun Claude bersamaan dengan satu config global, **4b** fallback provider berbasis kebijakan. Menyerap poin usage-limit polecat yang sebelumnya ada di E16 (d). **Direvisi di Revisi 2** (`13`): semua akun wajib memakai satu config global per runtime; hanya kredensial yang berbeda (invariant I1–I7).
2. **Masalah dan skenario gagal.**
   - Skenario 1: tiga polecat memakai akun Claude yang sama. Akun mencapai limit di tengah implementasi. Witness melihat sesi diam, menganggapnya stalled, lalu me-restart berulang ke akun yang sama.
   - Skenario 2: `quota_dog` dinyalakan. Polecat dirotasi ke akun B dengan menyalin token B ke entri keychain default, yaitu entri yang juga dikelola claude-swap. Status limited akun A tidak disimpan, sehingga rotasi berikutnya bisa memilih A lagi.
   - Skenario 2b: akun ditambahkan lewat `gt account add`. Sesi pada akun itu berjalan dengan config dir yang hampir kosong, sehingga skills, rules, plugins, hooks user, MCP, dan history tidak tersedia (`13` K22).
   - Skenario 3: semua akun Claude terkena limit pada jam sibuk. Pekerjaan berhenti sampai reset, padahal Anda punya akses provider lain.
3. **Bukti.**
   - [V] `gt account` dan `gt quota scan|rotate|watch` ada (`cmd/account.go:24-39`, `cmd/quota.go`). Scanner membaca semua sesi termasuk polecat (`quota/scan.go:100-117`).
   - [V] Rotasi produksi menyalin token keychain (`quota/keychain.go:85-110`, `:125-178`); token lewat argumen command line (`:66-71`).
   - [V] Status limited tidak disimpan setelah rotasi (`cmd/quota.go:424-428`); `MarkLimited` hanya dipanggil test.
   - [V] Rotator berbasis swap config dir + `--resume` hanya dipanggil test (`quota/executor.go:40-261`).
   - [V] `IsClaudeUsageLimit` hanya dipakai untuk Deacon (`daemon/usage_limit.go:31-41`, `daemon.go:1665`); [U] #4887.
   - [V] Akun tanpa field provider (`config/types.go:1531-1542`); `ConfigDirEnv` hanya untuk claude/copilot/groq (`config/agents.go:245,422,528`).
   - [V] Handoff tidak bisa ganti runtime: `agentOverride` selalu `""` (`cmd/handoff.go:1001`, `:260,292`).
   - [V] Tidak ada test untuk `cmd/quota` rotate.
4. **Kondisi saat ini dan gap.**
   - Ada & jalan: registrasi akun, isolasi `CLAUDE_CONFIG_DIR` per sesi, scanner limit, pemilihan akun LRU, 13 preset runtime, `RoleAgents` per role/rig.
   - Ada, belum terhubung: `MarkLimited`. Rotator swap config dir juga ada, tetapi **tidak dipakai** karena memecah config (`13` K21).
   - Gap: akun bersamaan tanpa memecah config; klasifikasi limit untuk polecat; persistensi status limited; daftar fallback runtime; strategi kredensial per provider; handoff lintas runtime.
5. **Alasan diprioritaskan.** Keputusan Anda (Revisi 1). Secara teknis 4a sebagian besar wiring atas kode yang sudah ada, dan memperbaiki mekanisme rotasi yang berisiko bila dinyalakan apa adanya.
6. **Dampak ke workflow Anda.**
   - Limit akun tidak lagi menghentikan polecat atau memicu restart berulang.
   - Pekerjaan berlanjut di akun lain atau provider lain, dengan konteks dari bead + branch yang sudah di-push.
   - Setiap sesi tercatat memakai akun dan provider mana (masukan untuk E08/E14 kelak).
7. **Scope minimum.** Detail dan alasan pemilihan opsi ada di `13` § 5–7.
   - **4a — Akun Claude bersamaan dengan satu config global (opsi A: token per proses).**
     - (a) Registry akun menyimpan referensi item Keychain per handle (token dari `claude setup-token`), bukan token dan bukan config dir.
     - (b) Launcher mengambil token saat `exec`, men-set `CLAUDE_CODE_OAUTH_TOKEN`, meng-unset `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`/`CLAUDE_CONFIG_DIR`. Semua akun memakai `~/.claude` dan `~/.claude.json` yang sama. Token tidak masuk string respawn tmux.
     - (c) Witness/daemon memakai `IsClaudeUsageLimit` untuk polecat. Status sesi menjadi `limited`, bukan stalled.
     - (d) Failover: restart dengan akun non-limited berikutnya + `--resume <session-id>`; transkrip ditemukan karena `projects/` sama.
     - (e) Status limited disimpan dengan waktu reset (dari pane atau dari `cswap list --json` bila tersedia).
     - (f) Spawn tanpa akun eksplisit memilih akun non-limited dengan usage terendah.
     - (g) Untuk Claude, matikan rotasi keychain, `gt account switch`, dan pembuatan config dir oleh `gt account add`. Gas Town tidak menyentuh entri Keychain default dan `oauthAccount` (wilayah claude-swap).
     - (h) Test alur rotate, termasuk "semua akun limited → status `limited` + eskalasi, bukan loop".
   - **4b — Fallback provider.**
     - (a) Spike per provider sebelum kode (login berlangganan, Q10): Codex (`CODEX_HOME` per akun + denylist symlink + perilaku SQLite/WAL) dan Gemini (`GEMINI_CLI_HOME` + lokasi kredensial).
     - (b) `credential_strategy` per preset (`token-env` | `home-per-account` | `api-key-env`) dan `ConfigDirEnv` yang benar per preset. Saat ini `AgentEnv` selalu memakai nama `CLAUDE_CONFIG_DIR`.
     - (c) Konfigurasi `role_agents_fallback` per role/rig, mis. `polecat: [claude, codex]`.
     - (d) Pola deteksi limit per provider.
     - (e) `gt handoff --agent <preset>` (mengisi `agentOverride` yang sudah ada). Sesi baru dibangun dari bead + branch, bukan transkrip.
     - (f) `gt doctor` memverifikasi bahwa setiap home per akun hanya berisi kredensial + symlink ke config global.
     - (g) Event `session.lifecycle` mencatat `{account, provider}`.
8. **Di luar scope:** kapasitas per akun/provider di scheduler (E02); routing berbasis kualitas model (E08/E14); dukungan Linux; perbaikan `account switch` (dicatat sebagai risiko, bukan dikerjakan).
9. **Dependensi dan urutan.** 4a setelah P0.3 (akun terdaftar). Bersentuhan dengan file Rank 5 (witness/daemon), jadi koordinasikan urutan merge. 4b setelah 4a dan setelah Q10 terjawab.
10. **Effort:** 4a **M**, 4b **M–L**. Keyakinan: sedang untuk 4a (deteksi limit dan pemilihan akun sudah ada; launcher token baru dan perlu diuji), rendah untuk 4b (perilaku kredensial dan SQLite provider lain belum diverifikasi; prime lewat nudge untuk preset tanpa hooks belum diuji).
11. **Trade-off, risiko, rollout/rollback.**
    - Lanjut di provider lain dapat menurunkan kualitas hasil. Mitigasi: fallback hanya untuk role/rig yang Anda izinkan, dan gate Rank 2 tetap berlaku sama.
    - Token per proses tidak mendukung claude.ai connectors dan Remote Control [D]. Q12 dijawab tidak dibutuhkan. Bila kelak dibutuhkan, 4a beralih ke home per akun dengan denylist symlink (`13` § 5 opsi C).
    - `setup-token` berlaku 1 tahun dan dirotasi manual. `gt doctor` memperingatkan token yang mendekati kedaluwarsa.
    - Kebijakan provider untuk multi-akun perlu Anda cek sendiri (`12` § 3.2).
    - Rollout: 4a dengan `quota_dog` on di pilot, dan pantau event `limited`. 4b mulai dari satu rig.
    - Rollback: `quota_dog` off; hapus `role_agents_fallback`.
12. **Acceptance criteria.**
    - [ ] Test: pane polecat berisi pesan usage limit → status `limited`, tidak ada restart ke akun yang sama.
    - [ ] Test: rotasi memilih akun non-limited lain, menyimpan status limited akun asal, dan tidak menulis entri Keychain default maupun `oauthAccount`.
    - [ ] Dua polecat berjalan bersamaan dengan akun berbeda dan memakai `~/.claude` + `~/.claude.json` yang sama. Tidak ada config dir baru yang dibuat Gas Town.
    - [ ] `cswap switch` saat polecat berjalan tidak mengubah akun polecat.
    - [ ] Rotasi melanjutkan transkrip yang sama (`--resume`). Token tidak muncul di `ps`, environment tmux, `accounts.json`, atau log.
    - [ ] Test: semua akun limited → tidak ada loop restart; eskalasi tercatat.
    - [ ] (4b) Polecat dengan fallback `[claude, codex]` yang semua akun Claude-nya limited di-respawn sebagai `codex`, dan prime/kontrak bead sampai ke sesi baru.
    - [ ] Event `session.lifecycle` memuat akun dan provider.
13. **Metrik.**
    - Waktu idle polecat karena limit (sebelum/sesudah).
    - Jumlah restart sia-sia karena limit.
    - First-pass gate rate per provider (data awal untuk E08/E14).

### Rank 5 — State fail-closed di boundary lifecycle

1. **Nama/ID:** E16 (kontrak state unknown/empty/active), dibatasi pada tiga boundary. Ditambah cek kepemilikan minimum dari E03. Poin usage-limit polecat pindah ke Rank 4a (Revisi 1).
2. **Masalah dan skenario gagal.**
   - Skenario 1: polecat `nux` selesai (`state=exiting`). Polecat itu lalu di-sling ulang untuk bead baru. Selama sesi baru menunggu Claude siap, sebelum heartbeat diperbarui, reaper melihat tmux hidup tetapi heartbeat lama dan `exiting`. Reaper membunuh sesi baru (#4890).
   - Skenario 2: Dolt lambat sesaat. `hasAssignedOpenWork` mengembalikan "tidak ada pekerjaan", sehingga reaper menganggap worker idle.
   - Skenario 3: bead di-force re-sling ke polecat lain. Sesi lama tetap menjalankan `gt done` dan mengirim MR untuk bead yang sama.
3. **Bukti.**
   - [V] `daemon/daemon.go:2827-2849` (error menjadi `false`). Graph: dipanggil `reapIdlePolecat` ← `reapRigIdlePolecats`.
   - [V] `session/heartbeat.go:33-38,101-113`: tanpa generation; error baca menjadi nil.
   - [V] `cmd/done.go:2570-2588`: error ditelan.
   - [V] `cmd/polecat.go:1078,1346`: hook kosong dianggap aman.
   - [U] #4890, #4632, #4631 masih OPEN.
4. **Kondisi saat ini dan gap.**
   - Keputusan nuke sebagian besar sudah fail-closed. Itu kekuatan yang perlu dipertahankan.
   - Gap ada di reaper sesi dan lookup assignment.
5. **Alasan diprioritaskan.** Setiap worker sehat yang dibunuh berarti kerja hilang, re-sling, dan intervensi manual. Frekuensinya naik seiring jumlah polecat, dan juga naik ketika Rank 4 me-respawn sesi lebih sering.
6. **Dampak ke workflow Anda.**
   - Lebih sedikit sesi yang mati misterius.
   - `gt done` dari sesi usang ditolak.
7. **Scope minimum.**
   - (a) Heartbeat menyimpan `session_id`/generation. Reaper mengabaikan heartbeat dari generation lain, dan `Start` menghapus atau menulis ulang heartbeat sebelum menunggu runtime.
   - (b) `hasAssignedOpenWork` dan `queryAssignedBeads` mengembalikan tri-state `{yes, no, unknown}`. `unknown` tidak pernah memicu kill atau cleanup.
   - (c) `gt done` memverifikasi bahwa bead masih ter-assign ke agent ini. Jika `unknown` atau milik agent lain: tolak dengan exit nonzero, tanpa push maupun MR.
8. **Di luar scope:** lease/fencing token penuh (E03); refactor seluruh pembacaan state; deteksi split-brain multi-socket (E30); usage-limit (Rank 4a).
9. **Dependensi dan urutan:** setelah P0.1. Independen dari Rank 1/2, jadi bisa paralel di Tahap 1. Merge sebelum Rank 4a karena menyentuh area daemon/witness yang sama.
10. **Effort:** M. Asumsi: 3 titik perubahan dengan test unit dan fault-injection (bd error, heartbeat usang). Keyakinan: sedang.
11. **Trade-off, risiko, rollout/rollback.**
    - Fail-closed bisa membuat sesi yang benar-benar macet hidup lebih lama. Mitigasinya batas waktu terpisah yang lebih panjang ditambah eskalasi, bukan kill.
    - Rollout bertahap per perubahan (a)–(c). Setiap perubahan bisa di-revert terpisah.
12. **Acceptance criteria.**
    - [ ] Test: heartbeat generation lama dengan `exiting` dan sesi baru hidup → reaper tidak membunuh.
    - [ ] Test: `bd list` error → reaper tidak membunuh, dan event `state_unknown` tercatat.
    - [ ] Test: `gt done` dari agent yang bukan assignee → exit nonzero, tidak ada push, tidak ada MR.
13. **Metrik.**
    - Jumlah sesi polecat yang di-kill reaper sementara bead-nya kemudian selesai normal oleh sesi berikutnya ("false kill").
    - Jumlah re-sling manual per minggu.

## 5. Quick wins (hardening kecil, bukan top 5)

Masing-masing XS–S. Boleh dikerjakan oportunistik saat menyentuh file terkait.

- Hapus atau implementasikan `on_conflict` dan `max_retry_count`. Config yang diam-diam tidak berlaku menyesatkan operator. [V]
- Fast-path `pre_verified` dijanjikan di formula polecat tetapi tidak ada di formula refinery. Selaraskan teks formula. [V] Selesai: PR #27 (gt-pd1c.56); `--pre-verified` informasional, refinery selalu menjalankan gate.
- Batasi konkurensi `runBdCmd` di dashboard dengan semaphore (#4820) bila dashboard dipakai. [V]
- `gt doctor` ikut menjalankan cek embedded-vs-store `bd_version`, yang saat ini hanya ada di daemon. [V]
- Tambahkan formula `mol-validate-prd` yang dirujuk `appendValidationWave` (`cmd/convoy_stage.go:1094-1097`) atau hapus rujukannya. [V, Revisi 1]
- Hubungkan `TriggerSynthesisIfReady` (`cmd/synthesis.go:692`, tanpa caller) agar sintesis convoy berjalan otomatis. Berguna untuk briefing Rank 3. [V, Revisi 1]
