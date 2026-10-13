# Gas Town — Enhancement Tambahan dari Audit Open Issues

Tanggal: 12 Oktober 2026. Scope: tambahan terhadap E01–E15, bukan pengganti backlog awal.

## Cara membaca

Prioritas adalah rekomendasi untuk kebutuhan pengguna, bukan label upstream. Impact bersifat kualitatif dan belum dibenchmark. Laporan bug belum direproduksi pada mesin pengguna. Status PR terverifikasi berada di `04-open-issue-findings.md`.

Sebagian E16–E30 adalah area baru; sebagian memperkuat scope existing hingga menjadi invariant yang dapat ditegakkan. Jangan menganggap 15 tambahan ini seluruhnya fitur yang sama sekali belum ada di source.

| ID | Enhancement | Prioritas | Hubungan dengan backlog awal |
| --- | --- | --- | --- |
| E16 | Kontrak state yang membedakan unknown, empty, dan active | P0 | Perluasan E05/E09 menjadi invariant lintas-sistem |
| E17 | Compiler formula, validator, dan pengujian workflow end-to-end | P0 | Baru; melengkapi E04 dan E08 |
| E18 | Pesan antar-agent yang durable dan kanal kontrol terpisah | P0 | Baru; terkait E03 dan E09 |
| E19 | Lifecycle data, retention, dan restore yang dapat dibuktikan | P0 | Baru; melampaui checkpoint E09 dan adopsi E15 |
| E20 | Matriks kompatibilitas dan release yang dapat direproduksi | P0 | Perluasan E15 sepanjang lifecycle instalasi |
| E21 | Batas kepemilikan workspace dan isolasi mutasi Git | P0 | Baru; lebih kuat daripada koordinasi konflik E06 |
| E22 | Isolasi kegagalan dan pembatasan beban control plane | P0 | Perluasan E02/E13; fokus pada daemon/dashboard, bukan worker |
| E23 | Kebijakan eksekusi persisten dan mode fork yang eksplisit | P0 untuk local-only; P1 untuk mode lanjutan | Perluasan E04/E05 |
| E24 | Manifest artefak dan synthesis lintas-agent | P1 | Baru; melengkapi evidence E05 dan desain E12 |
| E25 | Workspace monorepo dan checkout sesuai kebutuhan | P1 bila repo besar; P2 lainnya | Baru; terkait E06/E10 |
| E26 | Agent dapat membaca observability aplikasi | P1 | Baru; berbeda dari dashboard produktivitas E13 |
| E27 | Integrasi event forge dan incoming-PR workflow | P2; P1 bila volume PR tinggi | Baru; memanfaatkan E11/E18 |
| E28 | Pause, drain, dan maintenance freeze yang durable | P0 sebelum migrasi/live operations | Perluasan E02/E15 dengan enforcement lintas-writer |
| E29 | Profil stack dan perintah build/test yang tervalidasi | P1 untuk stack pengguna | Baru; melengkapi E10 |
| E30 | Identitas kanonik town/rig/session dan pencegahan split-brain | P0 untuk operasi multi-instance | Perluasan E01/E03/E15 |

## E16 — Kontrak state yang membedakan unknown, empty, dan active

**Prioritas:** P0. **Hubungan:** Perluasan E05/E09 menjadi invariant lintas-sistem.

**Issue pendukung:** [#4632](https://github.com/gastownhall/gastown/issues/4632), [#4633](https://github.com/gastownhall/gastown/issues/4633), [#4631](https://github.com/gastownhall/gastown/issues/4631), [#4245](https://github.com/gastownhall/gastown/issues/4245), [#4597](https://github.com/gastownhall/gastown/issues/4597).

### Deskripsi

Bangun satu model observed-state dengan nilai state, sumber, waktu observasi, session ID, dan generation. Kesalahan baca, state kedaluwarsa, dan hasil kosong yang sah harus menghasilkan nilai berbeda.

### Impact

Mencegah keputusan salah seperti menghapus workspace karena query gagal atau menganggap worker idle berdasarkan sinyal lama.

### Insight dan rekomendasi

Prioritaskan action boundary pada done, cleanup, dispatch, dan recovery. Gunakan hook/protocol runtime bila tersedia; TUI scraping tetap fallback dengan confidence dan expiry eksplisit. Samakan semantik output JSON, teks, dan exit code.

### Acceptance criteria awal

- [ ] Query gagal menghasilkan unknown dan exit nonzero
- [ ] State dari sesi lama ditolak
- [ ] Unknown memblokir aksi destruktif tetapi tidak menghentikan diagnostik baca
- [ ] Rendering teks tidak boleh menampilkan SAFE_TO_NUKE untuk state lain.

### Tradeoff

Menghentikan aksi saat unknown dapat mengurangi availability sesaat. Scope penghentian harus terbatas pada operasi yang membutuhkan state tersebut.

## E17 — Compiler formula, validator, dan pengujian workflow end-to-end

**Prioritas:** P0. **Hubungan:** Baru; melengkapi E04 dan E08.

**Issue pendukung:** [#4909](https://github.com/gastownhall/gastown/issues/4909), [#4039](https://github.com/gastownhall/gastown/issues/4039), [#4032](https://github.com/gastownhall/gastown/issues/4032), [#3798](https://github.com/gastownhall/gastown/issues/3798), [#4886](https://github.com/gastownhall/gastown/issues/4886), [#4732](https://github.com/gastownhall/gastown/issues/4732), [#4835](https://github.com/gastownhall/gastown/issues/4835), [#3322](https://github.com/gastownhall/gastown/issues/3322).

### Deskripsi

Perlakukan formula sebagai program yang dikompilasi menjadi execution plan. Validasi dialect template, variabel wajib, command/flag, role target, graph dependensi, jumlah step, dan lokasi output sebelum dispatch.

### Impact

Mengurangi workflow yang tampak sukses tetapi tidak menjalankan langkah, kehilangan parameter, atau salah menafsirkan dry-run.

### Insight dan rekomendasi

Gunakan execution plan yang sama untuk preview dan eksekusi. Tolak placeholder tidak ter-render kecuali literal yang dinyatakan eksplisit. Validasi nested workflow dan human gate; human gate tetap terikat pada sesi percakapan yang benar.

### Acceptance criteria awal

- [ ] Dry-run dan eksekusi memakai plan hash yang sama
- [ ] Formula yang mengharuskan step tidak boleh menghasilkan root kosong
- [ ] Target membutuhkan capability sesuai
- [ ] Command yang tidak tersedia ditolak sebelum worker dibuat.

### Tradeoff

Compiler bukan pengganti penalaran agent. Jangan menyimpulkan semua formula rusak karena beberapa dialect/path bermasalah.

## E18 — Pesan antar-agent yang durable dan kanal kontrol terpisah

**Prioritas:** P0. **Hubungan:** Baru; terkait E03 dan E09.

**Issue pendukung:** [#4634](https://github.com/gastownhall/gastown/issues/4634), [#4711](https://github.com/gastownhall/gastown/issues/4711), [#4713](https://github.com/gastownhall/gastown/issues/4713), [#4607](https://github.com/gastownhall/gastown/issues/4607), [#4107](https://github.com/gastownhall/gastown/issues/4107), [#4660](https://github.com/gastownhall/gastown/issues/4660), [#4225](https://github.com/gastownhall/gastown/issues/4225), [#4514](https://github.com/gastownhall/gastown/issues/4514), [#1216](https://github.com/gastownhall/gastown/issues/1216).

### Deskripsi

Terapkan message ID, delivery acknowledgement, deduplikasi, retry, expiry yang terlihat, serta antrean prioritas. Bedakan pesan informasional, assignment, acknowledgement, dan cancel/hold.

### Impact

Mengurangi koordinasi yang hilang diam-diam, gangguan terhadap tool yang sedang berjalan, serta wake-up semua rig akibat event yang salah scope.

### Insight dan rekomendasi

Gunakan pengiriman at-least-once dengan consumer idempotent. Simpan payload sebelum notifikasi. Pisahkan town/rig/session routing dan sediakan dead-letter queue. Kanal cancel/hold harus ditegakkan runtime, bukan sekadar pesan yang menunggu agent idle.

### Acceptance criteria awal

- [ ] Pesan urgent tidak hilang karena reminder memenuhi antrean
- [ ] Sender dapat melihat queued/delivered/acknowledged/expired
- [ ] Restart tidak kehilangan claim pesan
- [ ] Notifikasi rutin tidak membatalkan tool call
- [ ] Event satu rig tidak dikonsumsi rig lain.

### Tradeoff

Exactly-once end-to-end tidak boleh dijanjikan tanpa bukti. Broadcast dan competing consumer memerlukan semantik yang berbeda.

## E19 — Lifecycle data, retention, dan restore yang dapat dibuktikan

**Prioritas:** P0. **Hubungan:** Baru; melampaui checkpoint E09 dan adopsi E15.

**Issue pendukung:** [#4911](https://github.com/gastownhall/gastown/issues/4911), [#4830](https://github.com/gastownhall/gastown/issues/4830), [#4605](https://github.com/gastownhall/gastown/issues/4605), [#4397](https://github.com/gastownhall/gastown/issues/4397), [#4853](https://github.com/gastownhall/gastown/issues/4853), [#4411](https://github.com/gastownhall/gastown/issues/4411), [#4748](https://github.com/gastownhall/gastown/issues/4748), [#4889](https://github.com/gastownhall/gastown/issues/4889), [#4483](https://github.com/gastownhall/gastown/issues/4483).

### Deskripsi

Pisahkan status pekerjaan dari umur data. Terapkan retention berdasarkan jenis artefak dan referensi aktif, quarantine/tombstone sebelum purge, serta backup yang diuji melalui restore.

### Impact

Melindungi backlog, identitas agent, merge queue, alasan penolakan, dan pekerjaan yang belum di-push dari cleanup otomatis yang salah.

### Insight dan rekomendasi

Jangan menutup bug atau blocked task hanya karena tidak aktif. Periksa referensi parent-child dan MR. Cleanup harus berhenti jika preservation gagal. Backup harus mencakup data operasional yang tidak masuk riwayat Dolt; definisikan RPO/RTO setelah inventaris.

### Acceptance criteria awal

- [ ] Live MR dan blocked step tidak terkoleksi berdasarkan umur saja
- [ ] Preservation push gagal membatalkan penghapusan
- [ ] Backup salah konfigurasi terdeteksi
- [ ] Restore ke lingkungan kosong memulihkan ID dan dependensi yang disampel.

### Tradeoff

Retention tanpa batas meningkatkan biaya. Versioned database tidak otomatis melindungi tabel yang dikecualikan dari versioning.

## E20 — Matriks kompatibilitas dan release yang dapat direproduksi

**Prioritas:** P0. **Hubungan:** Perluasan E15 sepanjang lifecycle instalasi.

**Issue pendukung:** [#4789](https://github.com/gastownhall/gastown/issues/4789), [#4770](https://github.com/gastownhall/gastown/issues/4770), [#4727](https://github.com/gastownhall/gastown/issues/4727), [#3941](https://github.com/gastownhall/gastown/issues/3941), [#4142](https://github.com/gastownhall/gastown/issues/4142), [#4817](https://github.com/gastownhall/gastown/issues/4817), [#3416](https://github.com/gastownhall/gastown/issues/3416), [#3077](https://github.com/gastownhall/gastown/issues/3077).

### Deskripsi

Catat kompatibilitas antara binary gt, Beads yang di-embed, CLI bd, schema Dolt, runtime provider, dan tmux. Jalankan preflight sebelum store dibuka atau dimigrasikan.

### Impact

Mencegah upgrade salah pasangan, schema yang tidak terbaca, dan perbaikan lokal yang hilang saat restart image.

### Insight dan rekomendasi

Rilis bundle versi tervalidasi dan lockfile toolchain. Gunakan contract test untuk envelope JSON dan command flags. Tampilkan binary path, hash, schema range, serta drift. Bedakan bug sudah diperbaiki di main dari bug yang belum sampai release.

### Acceptance criteria awal

- [ ] Pasangan inkompatibel ditolak sebelum migrasi
- [ ] Restart memakai versi yang sama
- [ ] Parser diuji pada envelope yang didukung
- [ ] Upgrade dapat dicoba pada salinan data dengan prosedur rollback yang sesuai kemampuan database.

### Tradeoff

Jangan menyarankan downgrade binary sebagai rollback schema secara otomatis. Laporan versi dalam issue berlaku pada lingkungan pelapor, bukan seluruh instalasi.

## E21 — Batas kepemilikan workspace dan isolasi mutasi Git

**Prioritas:** P0. **Hubungan:** Baru; lebih kuat daripada koordinasi konflik E06.

**Issue pendukung:** [#4404](https://github.com/gastownhall/gastown/issues/4404), [#4602](https://github.com/gastownhall/gastown/issues/4602), [#4094](https://github.com/gastownhall/gastown/issues/4094), [#4551](https://github.com/gastownhall/gastown/issues/4551), [#4535](https://github.com/gastownhall/gastown/issues/4535), [#4672](https://github.com/gastownhall/gastown/issues/4672), [#4209](https://github.com/gastownhall/gastown/issues/4209), [#3737](https://github.com/gastownhall/gastown/issues/3737).

### Deskripsi

Ikat run ke repository identity, worktree identity, allowed write roots, dan target remote. Validasi sebelum git config, checkout, reset, clean, commit, push, atau operasi ref dijalankan.

### Impact

Mencegah agent mengubah clone lain, town root, remote yang salah, atau meng-commit conflict markers ketika checkpoint berjalan.

### Insight dan rekomendasi

Gunakan argumen terstruktur dan canonical path. Untuk batas kuat, gunakan isolasi filesystem/process sesuai platform; regex shell saja tidak memberi perlindungan penuh. Pisahkan runtime scaffolding dari artefak produk dan deteksi file scaffolding yang sudah tracked.

### Acceptance criteria awal

- [ ] Write ke workspace worker lain ditolak
- [ ] Read-only diagnosis tetap bisa
- [ ] Symlink dan beberapa git -C ditangani
- [ ] Checkpoint mengenali sequencer aktif
- [ ] Push memastikan remote dan branch sesuai kebijakan.

### Tradeoff

Sandbox menambah kompleksitas toolchain dan cache. Worktree memisahkan perubahan tetapi bukan security boundary.

## E22 — Isolasi kegagalan dan pembatasan beban control plane

**Prioritas:** P0. **Hubungan:** Perluasan E02/E13; fokus pada daemon/dashboard, bukan worker.

**Issue pendukung:** [#4731](https://github.com/gastownhall/gastown/issues/4731), [#4820](https://github.com/gastownhall/gastown/issues/4820), [#4292](https://github.com/gastownhall/gastown/issues/4292), [#3396](https://github.com/gastownhall/gastown/issues/3396), [#4165](https://github.com/gastownhall/gastown/issues/4165), [#4106](https://github.com/gastownhall/gastown/issues/4106), [#4570](https://github.com/gastownhall/gastown/issues/4570).

### Deskripsi

Pisahkan kegagalan per rig dan per dependency. Batasi subprocess, koneksi, retry, polling, serta operasi bersamaan dari dashboard dan patrol.

### Impact

Menjaga rig sehat tetap produktif ketika satu store bermasalah dan mencegah dashboard menghabiskan kapasitas mesin.

### Insight dan rekomendasi

Gunakan single-flight per query, bounded process groups, cancellation yang membersihkan child, jitter/backoff, dan read model/cache dengan freshness. Store gagal harus menghasilkan unavailable, bukan antrean kosong yang sah.

### Acceptance criteria awal

- [ ] Satu rig gagal tidak menghentikan task independen di rig sehat
- [ ] Task yang bergantung pada rig gagal tetap tertahan
- [ ] Timeout membersihkan child process
- [ ] Dashboard idle memiliki batas penggunaan resource yang diuji.

### Tradeoff

Cache dapat membuat data basi. Setiap tampilan perlu observed_at serta penanda degraded; pool koneksi harus mengikuti antarmuka storage yang didukung.

## E23 — Kebijakan eksekusi persisten dan mode fork yang eksplisit

**Prioritas:** P0 untuk local-only; P1 untuk mode lanjutan. **Hubungan:** Perluasan E04/E05.

**Issue pendukung:** [#4512](https://github.com/gastownhall/gastown/issues/4512), [#4819](https://github.com/gastownhall/gastown/issues/4819), [#4739](https://github.com/gastownhall/gastown/issues/4739), [#4045](https://github.com/gastownhall/gastown/issues/4045), [#4442](https://github.com/gastownhall/gastown/issues/4442), [#4710](https://github.com/gastownhall/gastown/issues/4710).

### Deskripsi

Simpan push policy, merge destination, review policy, dan completion mode sebagai data durable. Sediakan mode local-only, report-only, human-review, product-fork, dan upstream-contribution sesuai kebutuhan.

### Impact

Memastikan maksud pengguna tetap berlaku setelah retry, redispatch, dan recovery; cocok langsung dengan rencana fork Gas Town.

### Insight dan rekomendasi

Tegakkan policy sebelum push, bukan baru di refinery. Product fork boleh memiliki branch produk sendiri; upstream-contribution biasanya menjaga branch tracking upstream tetap bersih. Mode ini harus dipilih eksplisit, bukan menganggap semua fork dilarang merge.

### Acceptance criteria awal

- [ ] Local-only tetap local saat redispatch
- [ ] Kegagalan baca policy menghentikan push
- [ ] Report-only tidak berubah menjadi completed code
- [ ] Rejection terminal berbeda dari request changes
- [ ] Bypass gate tidak dilakukan agent secara implisit.

### Tradeoff

Kebijakan pada aplikasi bukan pengganti pembatasan credential dan branch protection server. Konflik policy perlu aturan precedence yang jelas.

## E24 — Manifest artefak dan synthesis lintas-agent

**Prioritas:** P1. **Hubungan:** Baru; melengkapi evidence E05 dan desain E12.

**Issue pendukung:** [#303](https://github.com/gastownhall/gastown/issues/303), [#4032](https://github.com/gastownhall/gastown/issues/4032), [#4748](https://github.com/gastownhall/gastown/issues/4748).

### Deskripsi

Tiap task menghasilkan manifest output: jenis artefak, URI/path, hash, commit, produsen, versi input, retention, dan status validasi. Synthesis mengambil output melalui manifest, bukan menebak worktree.

### Impact

Mempercepat penggabungan desain, kode, test report, dan review; output tetap ditemukan setelah worker dibersihkan.

### Insight dan rekomendasi

Mulai dengan manifest kecil untuk commit dan laporan tes. Tambahkan gambar referensi serta screenshot untuk pipeline UI. Validasi referensi sebelum menutup run dan lindungi output yang masih dibutuhkan downstream.

### Acceptance criteria awal

- [ ] Synthesis menemukan semua output wajib tanpa pencarian manual
- [ ] Worktree cleanup tidak menghapus satu-satunya artefak
- [ ] Hash tidak cocok atau output hilang menghasilkan blocked, bukan sukses.

### Tradeoff

Ini memperluas metadata output yang mungkin sudah ada. PR lama tidak otomatis cocok untuk schema terbaru; hindari membuat task database kedua.

## E25 — Workspace monorepo dan checkout sesuai kebutuhan

**Prioritas:** P1 bila repo besar; P2 lainnya. **Hubungan:** Baru; terkait E06/E10.

**Issue pendukung:** [#1790](https://github.com/gastownhall/gastown/issues/1790), [#254](https://github.com/gastownhall/gastown/issues/254), [#4231](https://github.com/gastownhall/gastown/issues/4231), [#4010](https://github.com/gastownhall/gastown/issues/4010).

### Deskripsi

Tambahkan partial clone/sparse checkout opsional, pemetaan modul, dependent builds, dan kesiapan submodule saat reuse. Untuk multi-repo, catat kombinasi commit yang diuji.

### Impact

Mengurangi disk, startup time, serta build/test yang tidak terkait pada proyek modular.

### Insight dan rekomendasi

Pilih filter clone berdasarkan ukuran dan pola akses, jangan menjadikan treeless universal. Sparse checkout harus dapat diperluas ketika tes membutuhkan modul lain. Pertahankan satu rig untuk satu monorepo bila atomic commit diperlukan.

### Acceptance criteria awal

- [ ] Checkout memuat dependency yang dibutuhkan build
- [ ] Reuse menginisialisasi submodule
- [ ] PR memakai target branch yang dimaksud
- [ ] Hasil tes mencatat seluruh commit lintas-repo.

### Tradeoff

Atomic commit dalam satu monorepo berbeda dari koordinasi multi-repo. Jangan menjanjikan atomic merge lintas remote tanpa protokol tambahan.

## E26 — Agent dapat membaca observability aplikasi

**Prioritas:** P1. **Hubungan:** Baru; berbeda dari dashboard produktivitas E13.

**Issue pendukung:** [#1141](https://github.com/gastownhall/gastown/issues/1141).

### Deskripsi

Sediakan query terstruktur atas hasil tes, log aplikasi, trace, dan metric berdasarkan run/service/environment/version. Agent melakukan triage menggunakan bukti runtime selain diff kode.

### Impact

Membantu menemukan akar masalah integration/E2E, merutekan bug ke modul yang benar, dan mengurangi tebakan penyebab kegagalan.

### Insight dan rekomendasi

Mulai dari JUnit/test report dan log dev yang sudah disanitasi. Pisahkan test runner deterministik dari penilai hasil. Tambahkan korelasi trace setelah kebutuhan nyata terbukti.

### Acceptance criteria awal

- [ ] Bug yang dibuat membawa evidence, environment, dan commit
- [ ] Error sama dideduplikasi
- [ ] Secret tidak masuk konteks
- [ ] Hipotesis agent dibedakan dari fakta observasi.

### Tradeoff

Log tidak selalu menunjukkan akar masalah. Integrasi production memerlukan batas akses dan minimisasi data tersendiri.

## E27 — Integrasi event forge dan incoming-PR workflow

**Prioritas:** P2; P1 bila volume PR tinggi. **Hubungan:** Baru; memanfaatkan E11/E18.

**Issue pendukung:** [#743](https://github.com/gastownhall/gastown/issues/743), [#766](https://github.com/gastownhall/gastown/issues/766).

### Deskripsi

Terima event PR, review, CI, dan komentar untuk memicu triage atau melanjutkan task. Tambahkan workflow kontribusi masuk: klasifikasi, review awal, penilaian bukti, dan antrean keputusan.

### Impact

Memperpendek waktu tunggu feedback dan mengurangi polling serta pekerjaan manual maintainer fork.

### Insight dan rekomendasi

Verifikasi signature, deduplikasi delivery ID, dan lakukan reconciliation polling sebagai cadangan. Mulai dari triage/review; auto-merge hanya jika kebijakan dan gate eksplisit mengizinkan.

### Acceptance criteria awal

- [ ] Event berulang tidak membuat task ganda
- [ ] Event out-of-order direkonsiliasi dengan state forge
- [ ] Mapping repo-rig benar
- [ ] Komentar eksternal tidak menjadi perintah berwenang secara otomatis.

### Tradeoff

Tambah permukaan integrasi dan operasi jaringan. Belum prioritas jika bottleneck utama masih keandalan lokal.

## E28 — Pause, drain, dan maintenance freeze yang durable

**Prioritas:** P0 sebelum migrasi/live operations. **Hubungan:** Perluasan E02/E15 dengan enforcement lintas-writer.

**Issue pendukung:** [#4475](https://github.com/gastownhall/gastown/issues/4475), [#4471](https://github.com/gastownhall/gastown/issues/4471), [#4405](https://github.com/gastownhall/gastown/issues/4405), [#4407](https://github.com/gastownhall/gastown/issues/4407), [#3178](https://github.com/gastownhall/gastown/issues/3178).

### Deskripsi

Pisahkan pause dispatch, drain worker, freeze mutasi, dan stop role. Simpan desired state secara durable dengan audit perubahan dan generation.

### Impact

Membuat maintenance serta incident recovery dapat dilakukan tanpa daemon diam-diam memulai pekerjaan kembali.

### Insight dan rekomendasi

Pause harus diperiksa seluruh jalur dispatch. Freeze harus berada pada boundary mutasi bersama; jika direct bd di luar kontrol, laporkan batas itu dan verifikasi writer berhenti. Jangan mengklaim freeze town-wide dari command denylist.

### Acceptance criteria awal

- [ ] Pause bertahan setelah restart
- [ ] Daemon menghormati role disabled
- [ ] Write attempt selama freeze ditolak pada jalur yang dicakup
- [ ] Status menunjukkan writer yang masih aktif
- [ ] Read-only diagnosis tetap tersedia.

### Tradeoff

Freeze parsial berbahaya jika diberi nama seolah menyeluruh. Efek pada proses eksternal harus dinyatakan eksplisit.

## E29 — Profil stack dan perintah build/test yang tervalidasi

**Prioritas:** P1 untuk stack pengguna. **Hubungan:** Baru; melengkapi E10.

**Issue pendukung:** [#983](https://github.com/gastownhall/gastown/issues/983), [#3981](https://github.com/gastownhall/gastown/issues/3981), [#4585](https://github.com/gastownhall/gastown/issues/4585).

### Deskripsi

Deteksi stack serta package manager untuk menghasilkan kandidat perintah setup, lint, typecheck, test, dan build per modul. Mendukung kombinasi Java/Spring Boot, Flutter, Node, dan .NET.

### Impact

Mengurangi setup manual dan menghindari menjalankan default Go atau command kosong pada proyek lain.

### Insight dan rekomendasi

Utamakan wrapper serta script proyek yang sudah ada. Deteksi marker hanya menghasilkan kandidat, bukan bukti command benar. Jangan menimpa konfigurasi pengguna. Validasi profil melalui smoke run terisolasi.

### Acceptance criteria awal

- [ ] Monorepo multi-stack memiliki profil per modul
- [ ] Command wajib kosong ditolak dengan alasan
- [ ] Konfigurasi existing dipertahankan
- [ ] Package manager sesuai lockfile
- [ ] Profil mencatat versi toolchain.

### Tradeoff

Autodetection dapat salah pada repo contoh atau build khusus. Override eksplisit tetap menjadi sumber kebenaran.

## E30 — Identitas kanonik town/rig/session dan pencegahan split-brain

**Prioritas:** P0 untuk operasi multi-instance. **Hubungan:** Perluasan E01/E03/E15.

**Issue pendukung:** [#4637](https://github.com/gastownhall/gastown/issues/4637), [#4638](https://github.com/gastownhall/gastown/issues/4638), [#4916](https://github.com/gastownhall/gastown/issues/4916), [#3341](https://github.com/gastownhall/gastown/issues/3341), [#2682](https://github.com/gastownhall/gastown/issues/2682), [#2423](https://github.com/gastownhall/gastown/issues/2423), [#4598](https://github.com/gastownhall/gastown/issues/4598).

### Deskripsi

Gunakan ID internal stabil untuk town, rig, worker, dan session generation. Prefix task, alias, path, dan socket menjadi atribut routing yang divalidasi, bukan satu-satunya identitas.

### Impact

Mengurangi dua koordinator aktif tanpa disadari, salah routing database, serta benturan prefix antar-rig.

### Insight dan rekomendasi

Persist canonical registry dan socket locator. Terapkan ownership lease/fencing untuk koordinator per scope; tampilkan duplicate detection tanpa otomatis membunuh sesi. Canonicalize path dan normalisasi alias di satu boundary.

### Acceptance criteria awal

- [ ] Dua path symlink menuju town sama tidak menghasilkan dua otoritas
- [ ] Prefix bertabrakan ditolak
- [ ] Prefix hyphen/underscore dirutekan benar
- [ ] Alias deacon dan deacon/ dinormalisasi
- [ ] Sesi town lain tidak dianggap zombie milik town ini.

### Tradeoff

Nama sesi yang sama pada dua town sah tidak otomatis split-brain. Scope dan identitas town harus dibuktikan sebelum recovery.
