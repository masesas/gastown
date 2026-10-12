# Gas Town Fork — Backlog Enhancement

Tanggal: 12 Oktober 2026. Dasar: audit statis pada snapshot dan sumber dalam `01-summary.md`.

## Scope dokumen

Daftar enhancement sebagai dasar epic dan task. Semua impact bersifat estimasi kualitatif; belum ada benchmark yang membuktikan persentase peningkatan. Effort menunjukkan kompleksitas relatif, bukan komitmen durasi.

## Definisi prioritas

- **P0:** fondasi keandalan sebelum operasi paralel diperluas.
- **P1:** peningkatan throughput, kualitas, dan pemulihan setelah fondasi tersedia.
- **P2:** optimasi dan kemudahan operasional; dapat naik jika menjadi prasyarat pengguna.

Dependensi menunjukkan urutan desain, bukan larangan mengerjakan prototipe secara paralel. Sebagian mekanisme existing dapat memenuhi acceptance criteria sehingga cukup diperkuat, bukan ditulis ulang.

## Daftar ringkas

| ID | Enhancement | Prioritas | Impact | Effort |
| --- | --- | --- | --- | --- |
| E01 | Pengelolaan akun lintas-provider | P0 | Sangat tinggi | Tinggi |
| E02 | Scheduler berdasarkan kapasitas nyata | P0 | Sangat tinggi | Tinggi |
| E03 | Kepemilikan task atomik | P0 | Sangat tinggi | Tinggi |
| E04 | Kontrak task terstruktur | P0 | Sangat tinggi | Sedang |
| E05 | Kelulusan berdasarkan bukti | P0 | Sangat tinggi | Tinggi |
| E06 | Penjadwalan berdasarkan potensi konflik | P1 | Tinggi | Tinggi |
| E07 | Konteks bersama dengan versi | P1 | Tinggi | Sedang–tinggi |
| E08 | Routing sesuai kemampuan | P1 | Tinggi | Tinggi |
| E09 | Recovery dengan checkpoint | P1 | Tinggi | Tinggi |
| E10 | Lingkungan tes terisolasi | P1 | Tinggi | Sedang–tinggi |
| E11 | Review berdasarkan risiko | P1 | Tinggi | Sedang |
| E12 | Pipeline desain sampai verifikasi visual | P1 | Tinggi untuk UI | Sedang–tinggi |
| E13 | Dashboard produktivitas | P2 | Tinggi untuk optimasi | Sedang |
| E14 | Evaluasi routing dari riwayat hasil | P2 | Sedang–tinggi | Tinggi |
| E15 | Adopsi Beads/Dolt existing yang aman | P2; P0 bila adopsi menjadi prasyarat | Sangat tinggi untuk data existing | Sedang–tinggi |

## E01 — Pengelolaan akun lintas-provider

| Atribut | Nilai |
| --- | --- |
| Prioritas | P0 |
| Impact | Sangat tinggi |
| Effort | Tinggi |
| Dependensi | Tidak ada |
| Dasar riset | S2, S3, S9; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Pisahkan provider, akun autentikasi, runtime, model, worker, dan run. Worker mempertahankan identitas ketika sesi atau akun berubah. Sediakan adapter lifecycle untuk spawn, status, cancel, resume, dan pemeriksaan capability.

Credential dan state sesi terisolasi per akun/run. Aturan proyek dan skills dibagikan melalui paket konteks berversi. Deteksi kapasitas harus mencantumkan sumber serta tingkat kepastian; kuota yang tidak diketahui tidak boleh diperlakukan sebagai tak terbatas.

### Impact yang diharapkan

Memungkinkan beberapa akun dipakai bersamaan tanpa tertukar dan mengurangi intervensi saat pergantian akun.

### Insight dan tradeoff

Adapter berbeda antar-provider; global account switching dapat memengaruhi sesi lain. Antigravity perlu verifikasi tersendiri.

### Acceptance criteria awal

- [ ] Dua worker dengan akun berbeda berjalan simultan tanpa mengubah credential satu sama lain
- [ ] Pergantian akun mempertahankan worker ID dan task
- [ ] Kegagalan autentikasi tercatat tanpa membocorkan secret.

### Rekomendasi

Mulai dari adapter Claude existing dan satu runtime tambahan. Gunakan autentikasi yang didukung runtime; jangan menganggap seluruh provider memiliki mekanisme resume yang sama.

## E02 — Scheduler berdasarkan kapasitas nyata

| Atribut | Nilai |
| --- | --- |
| Prioritas | P0 |
| Impact | Sangat tinggi |
| Effort | Tinggi |
| Dependensi | E01; metadata task minimum dari E04 |
| Dasar riset | S4, S5, S6, S13; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Perluas scheduler global menjadi admission control per akun, provider, rig, dan sumber daya. Pisahkan kapasitas coding, review, build, browser, emulator, serta integration test.

Tambahkan prioritas dependency path, fairness antrean, backpressure dari merge queue, event-triggered dispatch dengan polling cadangan, dan graceful drain. Cadangkan kapasitas untuk koordinasi dan recovery.

### Impact yang diharapkan

Menekan rate limit dan resource exhaustion; meningkatkan throughput pekerjaan yang benar-benar selesai.

### Insight dan tradeoff

Scheduler terlalu kompleks bisa menyebabkan starvation atau kapasitas menganggur. Nilai quota/biaya yang tidak tersedia harus tetap ditandai unknown.

### Acceptance criteria awal

- [ ] Task tidak dijalankan ketika resource wajib penuh
- [ ] Task prioritas rendah tetap mendapat giliran
- [ ] Penurunan limit menghentikan dispatch baru dan menunggu pekerjaan aktif selesai.

### Rekomendasi

Mulai dengan limit statis terukur dan alasan dispatch/penundaan yang dapat dibaca; tambahkan optimasi adaptif setelah baseline tersedia.

## E03 — Kepemilikan task atomik

| Atribut | Nilai |
| --- | --- |
| Prioritas | P0 |
| Impact | Sangat tinggi |
| Effort | Tinggi |
| Dependensi | Identitas worker/run dari E01 |
| Dasar riset | S4, S6, S10; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Perkuat claim task dengan transaksi atomik, lease, heartbeat, dan fencing token atau nomor generasi kepemilikan. Gunakan idempotency key untuk operasi dispatch dan pelaporan hasil.

Validasi kepemilikan saat perubahan state dan penerimaan hasil. Setelah lease habis dan task dialihkan, generasi lama tidak boleh melakukan transisi final. Reconcile hasil crash di antara claim, spawn, dan pencatatan sukses.

### Impact yang diharapkan

Mengurangi pekerjaan ganda dan hasil usang yang menimpa hasil sah.

### Insight dan tradeoff

Lease terlalu pendek memicu takeover palsu; terlalu panjang memperlambat recovery. Efek samping eksternal tidak otomatis menjadi exactly-once hanya karena ada lease.

### Acceptance criteria awal

- [ ] Dua dispatch bersamaan menghasilkan satu pemilik task
- [ ] Worker lama ditolak setelah takeover
- [ ] Retry penyelesaian tidak menggandakan transisi
- [ ] Crash setiap tahap dapat direkonsiliasi.

### Rekomendasi

Pertahankan mekanisme locking/idempotensi existing yang masih relevan. Terapkan fencing pada penerimaan hasil dan jalur merge, bukan hanya prompt agent.

## E04 — Kontrak task terstruktur

| Atribut | Nilai |
| --- | --- |
| Prioritas | P0 |
| Impact | Sangat tinggi |
| Effort | Sedang |
| Dependensi | Tidak ada |
| Dasar riset | S4, S6, S8; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Standarkan task dengan tujuan, scope, non-goals, acceptance criteria, dependensi, kontrak API/schema/desain, versi input, evidence wajib, dan batas percobaan.

Validasi kelengkapan sebelum dispatch. Kriteria harus memeriksa perilaku, bukan hanya keberadaan file. Bedakan selesai implementasi, verified, siap merge, dan merged; detail tahap dapat disimpan sebagai metadata agar kompatibel dengan Beads.

### Impact yang diharapkan

Mengurangi salah tafsir dan scope creep; memudahkan implementer, tester, dan reviewer bekerja dari tujuan yang sama.

### Insight dan tradeoff

Template terlalu panjang meningkatkan overhead. Tugas sederhana tidak membutuhkan detail sebesar perubahan kontrak bisnis.

### Acceptance criteria awal

- [ ] Task yang tidak memiliki tujuan atau kriteria kelulusan ditandai belum siap
- [ ] Semua agent menerima versi kontrak yang sama
- [ ] Perubahan scope tercatat dan memicu penilaian dampak.

### Rekomendasi

Gunakan template kecil sebagai default dan field tambahan berdasarkan risiko. Untuk RentalPro, tulis kasus batas periode reservasi dan RBAC sebagai perilaku eksplisit.

## E05 — Kelulusan berdasarkan bukti

| Atribut | Nilai |
| --- | --- |
| Prioritas | P0 |
| Impact | Sangat tinggi |
| Effort | Tinggi |
| Dependensi | E03, E04 |
| Dasar riset | S7, S8; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Hubungkan acceptance criteria dengan hasil tes, review, dan artefak. Catat commit sumber, baseline target, konfigurasi tes, lingkungan, exit code, dan referensi log.

Perluas quality gates existing. Evidence harus diverifikasi oleh runner atau proses penerima, bukan semata klaim worker. Perubahan kode, baseline, atau konfigurasi mengharuskan validasi ulang sesuai dampaknya.

### Impact yang diharapkan

Mengurangi false completion dan reuse hasil tes yang tidak lagi relevan.

### Insight dan tradeoff

Invalidasi terlalu luas memboroskan waktu; terlalu sempit meloloskan regresi. Bukti lolos tes bukan bukti semua perilaku benar.

### Acceptance criteria awal

- [ ] Kegagalan gate wajib memblokir kelulusan
- [ ] Bukti untuk commit lain ditolak
- [ ] Setiap kriteria wajib memiliki evidence
- [ ] Pengecualian eksplisit tercatat.

### Rekomendasi

Awali dengan gate build, lint, tes relevan, dan review perubahan berisiko. Gunakan kembali pipeline refinery, termasuk post-squash verification bila diperlukan.

## E06 — Penjadwalan berdasarkan potensi konflik

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Tinggi |
| Dependensi | E02, E04 |
| Dasar riset | S4, S7; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Tentukan scope perubahan sebelum dispatch dan bandingkan dengan perubahan aktual. Identifikasi konflik pada file bersama, modul, schema, kontrak API, dan design tokens.

Gunakan reservasi scope terbatas, dependency graph, serta pemeriksaan diff. Perubahan di luar scope meminta penjadwalan ulang atau review; jangan mengunci seluruh repository.

### Impact yang diharapkan

Mengurangi konflik merge, rework, dan perubahan semantik yang saling membatalkan.

### Insight dan tradeoff

Prediksi scope tidak selalu tepat. Penguncian berlebihan menghilangkan manfaat paralelisme.

### Acceptance criteria awal

- [ ] Perubahan kontrak yang sama dijalankan berurutan atau melalui dependency eksplisit
- [ ] Perubahan independen tetap paralel
- [ ] Diff di luar scope terdeteksi.

### Rekomendasi

Mulai dari area konflik tinggi: schema migration, API contract, dependency lockfile, dan komponen bersama.

## E07 — Konteks bersama dengan versi

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Sedang–tinggi |
| Dependensi | E01, E04 |
| Dasar riset | S9, S10, S15; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Buat paket konteks per repository, role, dan task yang berisi aturan, keputusan arsitektur, kontrak relevan, serta ringkasan progres. Sertakan sumber dan versi.

Pisahkan pengetahuan otoritatif dari hipotesis agent. Batasi ukuran konteks dan distribusikan bagian relevan saja. Perubahan dokumen sumber menandai paket terkait perlu diperbarui.

### Impact yang diharapkan

Mengurangi pembacaan berulang, kehilangan konteks, dan perbedaan instruksi antar-runtime.

### Insight dan tradeoff

Ringkasan bisa menghilangkan detail penting; memory tanpa sumber dapat menyebarkan kesimpulan salah.

### Acceptance criteria awal

- [ ] Setiap run mencatat versi konteks
- [ ] Handoff membawa aturan yang sama
- [ ] Sumber yang berubah terdeteksi
- [ ] Credential tidak masuk paket konteks.

### Rekomendasi

Satukan pengetahuan dan ledger pekerjaan, tetapi jangan memaksakan seluruh history native provider menjadi satu file writable.

## E08 — Routing sesuai kemampuan

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Tinggi |
| Dependensi | E01, E02, E04; dibantu E13/E14 |
| Dasar riset | S9, S11; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Pilih runtime/model berdasarkan capability wajib, jenis task, risiko, kapasitas akun, dan bukti performa pada proyek. Mulai dari aturan yang eksplisit.

Capability dapat mencakup tool execution, browser, image generation, vision, atau akses repository. Bedakan capability wajib dan preferensi; sediakan fallback yang memenuhi syarat.

### Impact yang diharapkan

Mengurangi assignment yang salah dan penggunaan model mahal pada pekerjaan sederhana.

### Insight dan tradeoff

Benchmark umum belum tentu mewakili codebase; pembagian provider berdasarkan merek dapat mengunci keputusan yang tidak optimal.

### Acceptance criteria awal

- [ ] Task image generation tidak diarahkan ke runtime tanpa tool yang sesuai
- [ ] Fallback memenuhi capability wajib
- [ ] Alasan routing terekam.

### Rekomendasi

Gunakan preferensi Claude/Gemini/GPT sebagai konfigurasi awal. Jangan mengklaim router desain upstream siap produksi tanpa memeriksa implementasinya.

## E09 — Recovery dengan checkpoint

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Tinggi |
| Dependensi | E01, E03, E04 |
| Dasar riset | S3, S14, S15; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Simpan checkpoint berisi task, branch, commit, perubahan lokal, hasil tes, blocker, dan langkah berikutnya. Bedakan crash, quota, menunggu dependensi, tes panjang, dan loop kegagalan.

Gabungkan heartbeat, aktivitas tool, subprocess, dan progres tahap. Terapkan retry terbatas berdasarkan kategori kegagalan; jangan restart hanya karena terminal diam.

### Impact yang diharapkan

Mengurangi pengulangan kerja dan penghentian agent sehat.

### Insight dan tradeoff

Checkpoint yang tidak konsisten dapat kehilangan perubahan; output aktif belum tentu menandakan progres berarti.

### Acceptance criteria awal

- [ ] Worker dapat dilanjutkan dari checkpoint setelah crash
- [ ] Tes panjang tidak otomatis dianggap hung
- [ ] Error berulang mencapai batas retry dan menghasilkan blocker terarah.

### Rekomendasi

Uji recovery dengan fault injection pada boundary claim, spawn, checkpoint, verifikasi, dan merge.

## E10 — Lingkungan tes terisolasi

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Sedang–tinggi |
| Dependensi | E02, E04, E05 |
| Dasar riset | S7; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Sediakan identitas lingkungan per run untuk database, schema, port, fixture, browser profile, emulator, dan direktori output sesuai kebutuhan tes.

Batasi concurrency resource mahal. Bersihkan lingkungan setelah selesai dengan mekanisme cleanup yang dapat dilanjutkan. Resource bersama yang mutable memerlukan koordinasi eksplisit.

### Impact yang diharapkan

Menekan flaky test karena kontaminasi data dan tabrakan resource.

### Insight dan tradeoff

Isolasi penuh meningkatkan penggunaan RAM/disk; cache writable bersama juga bisa menimbulkan race.

### Acceptance criteria awal

- [ ] Dua suite integration dapat berjalan tanpa mencampur data
- [ ] Port tidak bertabrakan
- [ ] Run gagal tidak meninggalkan resource tanpa jejak
- [ ] Hasil tes mencatat environment ID.

### Rekomendasi

Untuk stack Spring Boot dan mobile, mulai dari database serta browser profile per run, lalu ukur kapasitas sebelum menambah emulator.

## E11 — Review berdasarkan risiko

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi |
| Effort | Sedang |
| Dependensi | E04, E05 |
| Dasar riset | S8; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Pilih aspek reviewer berdasarkan perubahan, lalu deduplikasi hasil berdasarkan lokasi, akar masalah, dan dampak. Tetapkan keputusan temuan: valid, perlu bukti, duplikat, atau tidak diterapkan dengan alasan.

Gunakan implementer dan reviewer terpisah untuk perubahan berisiko. Batasi siklus revisi; perubahan berikutnya memicu review ulang yang relevan.

### Impact yang diharapkan

Mengurangi token review yang tidak produktif dan revisi berputar.

### Insight dan tradeoff

Banyak reviewer dapat menghasilkan temuan berulang; kesepakatan antar-agent bukan bukti kebenaran.

### Acceptance criteria awal

- [ ] Perubahan pembayaran/RBAC memicu review terkait
- [ ] Temuan duplikat digabung
- [ ] Temuan blocker ditutup dengan evidence
- [ ] Batas iterasi menghasilkan eskalasi yang jelas.

### Rekomendasi

Gunakan formula review existing. Jangan menjalankan seluruh aspek review untuk perubahan kecil yang tidak memerlukannya.

## E12 — Pipeline desain sampai verifikasi visual

| Atribut | Nilai |
| --- | --- |
| Prioritas | P1 |
| Impact | Tinggi untuk UI |
| Effort | Sedang–tinggi |
| Dependensi | E04, E05, E08, E10 |
| Dasar riset | S9; rekomendasi pipeline khusus kebutuhan pengguna; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Hubungkan brief, gambar referensi, design tokens, implementasi, screenshot, dan verifikasi perilaku. Simpan versi artefak desain yang menjadi acuan implementasi.

Sediakan adapter image generation terpisah. Verifikasi juga loading, error, empty state, validasi form, navigasi, aksesibilitas, dan RBAC.

### Impact yang diharapkan

Meningkatkan kesesuaian UI dengan desain serta mencegah UI cantik tetapi perilakunya salah.

### Insight dan tradeoff

Gambar generatif tidak selalu konsisten secara layout atau dapat diterjemahkan langsung menjadi komponen. Pixel similarity saja tidak mengukur UX.

### Acceptance criteria awal

- [ ] Implementasi menyebut versi desain
- [ ] Screenshot berasal dari aplikasi yang berjalan
- [ ] State penting diuji
- [ ] Warna dan komponen mengikuti token proyek.

### Rekomendasi

Untuk RentalPro, gunakan satu flow representatif dahulu. Turunkan desain menjadi spesifikasi komponen dan perilaku sebelum dispatch implementasi.

## E13 — Dashboard produktivitas

| Atribut | Nilai |
| --- | --- |
| Prioritas | P2 |
| Impact | Tinggi untuk optimasi |
| Effort | Sedang |
| Dependensi | E04, E05; korelasi run dari E01 |
| Dasar riset | S12; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Agregasikan lead time, queue time, first-pass success, rework, konflik, intervensi manual, dan biaya terukur per pekerjaan tervalidasi.

Hubungkan task, run, akun, runtime, commit, evidence, dan merge. Bedakan angka terukur dari estimasi; tambahkan denominator serta periode pengukuran.

### Impact yang diharapkan

Memperlihatkan bottleneck dan mencegah optimasi berdasarkan jumlah agent semata.

### Insight dan tradeoff

Telemetry yang tidak lengkap dapat membentuk kesimpulan keliru. Data token/biaya mungkin tidak tersedia di semua runtime.

### Acceptance criteria awal

- [ ] Operator dapat menelusuri task lambat ke waktu tunggu atau rework
- [ ] Tidak ada biaya unknown yang ditampilkan sebagai nol
- [ ] Outcome merged dapat dibedakan dari done.

### Rekomendasi

Logging minimum dimulai sejak baseline. Dashboard lengkap dapat ditunda sampai data dan workflow stabil.

## E14 — Evaluasi routing dari riwayat hasil

| Atribut | Nilai |
| --- | --- |
| Prioritas | P2 |
| Impact | Sedang–tinggi |
| Effort | Tinggi |
| Dependensi | E08, E13 |
| Dasar riset | S11, S12; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Buat kumpulan task representatif dan nilai hasil berdasarkan kualitas, waktu, biaya terukur, serta kebutuhan perbaikan. Gunakan hasil untuk menyesuaikan routing.

Bandingkan task dengan tingkat kesulitan sebanding; pisahkan perubahan model, prompt, konteks, dan toolchain. Pertahankan evaluasi ulang saat versi berubah.

### Impact yang diharapkan

Menghasilkan penempatan agent yang sesuai pengalaman codebase, bukan asumsi merek model.

### Insight dan tradeoff

Sample kecil dan task berbeda dapat menghasilkan ranking menyesatkan; optimasi ke benchmark sempit dapat menurunkan generalisasi.

### Acceptance criteria awal

- [ ] Setiap perubahan routing memiliki hasil evaluasi dan versi konfigurasi
- [ ] Regresi terdeteksi
- [ ] Kebijakan dapat dikembalikan.

### Rekomendasi

Mulai dari evaluasi manual terstruktur. Hindari auto-routing adaptif sebelum jumlah sampel dan kualitas pencatatan memadai.

## E15 — Adopsi Beads/Dolt existing yang aman

| Atribut | Nilai |
| --- | --- |
| Prioritas | P2; P0 bila adopsi menjadi prasyarat |
| Impact | Sangat tinggi untuk data existing |
| Effort | Sedang–tinggi |
| Dependensi | Inventaris versi dan konfigurasi existing |
| Dasar riset | S10; lihat indeks sumber di 01-summary.md |

### Deskripsi dan scope

Buat pemeriksaan kompatibilitas Beads/Dolt, routing database, prefix task, metadata, dan konfigurasi Gas Town sebelum menghubungkan workspace existing.

Sediakan dry-run, snapshot/backup, daftar perubahan eksplisit, dan rollback. Pertahankan ID, dependensi, status, serta riwayat. Gunakan antarmuka yang didukung; hindari menulis schema internal secara spekulatif.

### Impact yang diharapkan

Mengurangi setup manual dan risiko task hilang atau masuk database yang salah.

### Insight dan tradeoff

Database town dan rig memiliki scope berbeda. Sekadar menggunakan server Dolt yang sama tidak menjamin task dirutekan dengan benar.

### Acceptance criteria awal

- [ ] Daftar ID dan dependensi penting konsisten sebelum/sesudah adopsi
- [ ] Task contoh dapat dibaca dari rig yang benar
- [ ] Rollback diuji pada salinan.

### Rekomendasi

Karena pengguna telah memakai Beads/Dolt, lakukan pemeriksaan kompatibilitas di awal. Jika akses data existing menghalangi pilot, naikkan item ini ke P0.

