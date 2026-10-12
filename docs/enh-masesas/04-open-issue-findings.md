# Gas Town — Temuan Audit Open Issues dan Rekomendasi Fork

Tanggal snapshot: **12 Oktober 2026**. Repository: [gastownhall/gastown](https://github.com/gastownhall/gastown).

## Kesimpulan

Peluang terbesar untuk produktivitas multi-agent adalah meningkatkan **pekerjaan benar yang selesai per satuan waktu dan biaya**, bukan sekadar jumlah sesi aktif. Laporan upstream menunjukkan beberapa titik lemah saling memperkuat: pembacaan state yang ambigu, komunikasi yang tidak andal, cleanup yang dapat menghilangkan pekerjaan, perbedaan versi storage, dan kebijakan yang tidak bertahan setelah redispatch.

Audit ini menghasilkan **15 tambahan/perluasan enhancement, E16–E30**, dengan acceptance criteria dan trade-off dalam `05-enhancement-tambahan.md`. Ini adalah rekomendasi desain berdasarkan laporan upstream; bukan pernyataan bahwa seluruh kemampuan tersebut sama sekali belum ada atau bahwa seluruh laporan telah direproduksi.

## Cakupan dan batas kepastian

| Pemeriksaan | Cakupan | Batas interpretasi |
| --- | --- | --- |
| Inventaris REST open items | 507 item, enam halaman pagination | Endpoint GitHub `/issues` juga mengandung PR |
| Issue setelah PR dikeluarkan | **375 issue open** | Status pada snapshot, dapat berubah kemudian |
| Screening | 375 judul dan isi/ringkasan laporan | Kedalaman tidak sama; bukan audit setiap komentar dan baris kode |
| Pendalaman terarah | **36 issue**, body dan timeline | Dipilih karena dampak pada correctness, orkestrasi, kehilangan kerja, dan peluang kapabilitas |
| Pemeriksaan kandidat perbaikan | **14 PR**, termasuk field `merged` | Tidak mencakup seluruh PR repository; tidak membuktikan tersedia dalam release |
| Reproduksi bug dan benchmark | Tidak dilakukan dalam audit ini | Impact kualitatif; tidak ada klaim persentase peningkatan |

Jawaban yang tepat atas “apakah semua sudah dianalisis?”: **semua 375 masuk inventaris dan screening, tetapi belum semua dibedah penuh sampai diskusi, reproduksi, dan code trace**. Indeks `06-open-issue-inventory.md` membedakan pendalaman dengan screening dan pemetaan awal. Pengelompokan awal bukan vonis bahwa sebuah issue pasti sudah tertangani oleh enhancement tertentu.

Audit source sebelumnya menggunakan commit `649b832b7672bc7a2dbef26f5983aba6198b819b`; jangan menyamakannya dengan HEAD atau release terbaru. Sebelum implementasi, pin commit fork, versi `gt`, `bd`, embedded Beads, Dolt, dan runtime agent; reproduksi masalah pada kombinasi tersebut.

## Temuan yang mengubah prioritas

1. **Correctness sebelum concurrency.** [#4632](https://github.com/gastownhall/gastown/issues/4632) melaporkan kegagalan baca hook diperlakukan seperti state kosong; [#4631](https://github.com/gastownhall/gastown/issues/4631) melaporkan representasi state yang keliru. Keputusan dispatch, done, cleanup, dan recovery membutuhkan state dengan asal, umur, dan generation yang jelas. E16 adalah fondasi E03/E05/E09.
2. **Dry-run harus mempunyai semantik yang dapat dipercaya.** [#4909](https://github.com/gastownhall/gastown/issues/4909) melaporkan ketidakcocokan template; [#4032](https://github.com/gastownhall/gastown/issues/4032) dan [#3798](https://github.com/gastownhall/gastown/issues/3798) memperlihatkan celah preview, rendering, dan target workflow. Jadikan formula execution plan yang divalidasi, bukan sekadar teks (E17).
3. **Pesan adalah bagian dari protokol kerja.** [#4634](https://github.com/gastownhall/gastown/issues/4634), [#4711](https://github.com/gastownhall/gastown/issues/4711), dan [#4607](https://github.com/gastownhall/gastown/issues/4607) melaporkan pesan hilang, tidak dipoll, atau menginterupsi tool. Gunakan pengiriman durable dengan ACK/idempotency serta kanal kontrol terpisah (E18).
4. **Backup belum berarti dapat restore.** [#4397](https://github.com/gastownhall/gastown/issues/4397), [#4830](https://github.com/gastownhall/gastown/issues/4830), [#4605](https://github.com/gastownhall/gastown/issues/4605), dan [#4853](https://github.com/gastownhall/gastown/issues/4853) mengarah ke retention berdasarkan liveness/reference, verifikasi preservasi, dan restore drill. Wisp yang diabaikan Dolt tidak boleh diasumsikan terlindungi history versioning (E19).
5. **Release compatibility adalah fitur produktivitas.** [#4789](https://github.com/gastownhall/gastown/issues/4789), [#4770](https://github.com/gastownhall/gastown/issues/4770), dan [#4727](https://github.com/gastownhall/gastown/issues/4727) mengindikasikan kebutuhan matriks versi dan preflight migrasi. Banyak laporan bisa berasal dari keluarga incompatibility yang sama; jangan menghitungnya sebagai banyak fitur independen (E20).
6. **Control plane bisa menjadi bottleneck sebelum model.** [#4820](https://github.com/gastownhall/gastown/issues/4820) melaporkan akumulasi proses dashboard; [#4731](https://github.com/gastownhall/gastown/issues/4731) kegagalan satu direktori menghentikan operasi lebih luas. Perlu isolasi per-rig, pembatasan proses/koneksi, cancellation dan backoff (E22).
7. **Kebijakan dan identitas harus bertahan lintas restart.** [#4512](https://github.com/gastownhall/gastown/issues/4512), [#4475](https://github.com/gastownhall/gastown/issues/4475), dan [#4637](https://github.com/gastownhall/gastown/issues/4637) mendasari durable policy, pause/drain, dan pencegahan split-brain (E23/E28/E30).

## Tambahan enhancement

Prioritas adalah rekomendasi untuk tujuan fork pengguna, bukan prioritas resmi upstream. P0 = fondasi sebelum memperbesar operasi atau melakukan operasi terkait; P1 = tahap produktivitas berikutnya; P2 = bersyarat pada kebutuhan. Banyak P0 bukan berarti semuanya dikerjakan bersamaan.

| ID | Area | Prioritas | Dampak utama |
| --- | --- | --- | --- |
| E16 | Kontrak state yang membedakan unknown, empty, dan active | P0 | Mencegah keputusan salah seperti menghapus workspace karena query gagal atau menganggap worker idle berdasarkan sinyal lama. |
| E17 | Compiler formula, validator, dan pengujian workflow end-to-end | P0 | Mengurangi workflow yang tampak sukses tetapi tidak menjalankan langkah, kehilangan parameter, atau salah menafsirkan dry-run. |
| E18 | Pesan antar-agent yang durable dan kanal kontrol terpisah | P0 | Mengurangi koordinasi yang hilang diam-diam, gangguan terhadap tool yang sedang berjalan, serta wake-up semua rig akibat event yang salah scope. |
| E19 | Lifecycle data, retention, dan restore yang dapat dibuktikan | P0 | Melindungi backlog, identitas agent, merge queue, alasan penolakan, dan pekerjaan yang belum di-push dari cleanup otomatis yang salah. |
| E20 | Matriks kompatibilitas dan release yang dapat direproduksi | P0 | Mencegah upgrade salah pasangan, schema yang tidak terbaca, dan perbaikan lokal yang hilang saat restart image. |
| E21 | Batas kepemilikan workspace dan isolasi mutasi Git | P0 | Mencegah agent mengubah clone lain, town root, remote yang salah, atau meng-commit conflict markers ketika checkpoint berjalan. |
| E22 | Isolasi kegagalan dan pembatasan beban control plane | P0 | Menjaga rig sehat tetap produktif ketika satu store bermasalah dan mencegah dashboard menghabiskan kapasitas mesin. |
| E23 | Kebijakan eksekusi persisten dan mode fork yang eksplisit | P0 untuk local-only; P1 untuk mode lanjutan | Memastikan maksud pengguna tetap berlaku setelah retry, redispatch, dan recovery; cocok langsung dengan rencana fork Gas Town. |
| E24 | Manifest artefak dan synthesis lintas-agent | P1 | Mempercepat penggabungan desain, kode, test report, dan review; output tetap ditemukan setelah worker dibersihkan. |
| E25 | Workspace monorepo dan checkout sesuai kebutuhan | P1 bila repo besar; P2 lainnya | Mengurangi disk, startup time, serta build/test yang tidak terkait pada proyek modular. |
| E26 | Agent dapat membaca observability aplikasi | P1 | Membantu menemukan akar masalah integration/E2E, merutekan bug ke modul yang benar, dan mengurangi tebakan penyebab kegagalan. |
| E27 | Integrasi event forge dan incoming-PR workflow | P2; P1 bila volume PR tinggi | Memperpendek waktu tunggu feedback dan mengurangi polling serta pekerjaan manual maintainer fork. |
| E28 | Pause, drain, dan maintenance freeze yang durable | P0 sebelum migrasi/live operations | Membuat maintenance serta incident recovery dapat dilakukan tanpa daemon diam-diam memulai pekerjaan kembali. |
| E29 | Profil stack dan perintah build/test yang tervalidasi | P1 untuk stack pengguna | Mengurangi setup manual dan menghindari menjalankan default Go atau command kosong pada proyek lain. |
| E30 | Identitas kanonik town/rig/session dan pencegahan split-brain | P0 untuk operasi multi-instance | Mengurangi dua koordinator aktif tanpa disadari, salah routing database, serta benturan prefix antar-rig. |

## Koreksi dan perluasan backlog E01–E15

| Backlog awal | Penajaman dari audit issue | Keputusan |
| --- | --- | --- |
| E01 multi-provider/account | Integrasi provider harus diverifikasi per runtime; permintaan dukungan tidak membuktikan provider lain ditutup | Tetap penting bila quota menjadi hambatan; jangan jadikan isu provider spekulatif sebagai dasar migrasi |
| E02 resource scheduler | Beban daemon, dashboard, subprocess, dan Dolt juga perlu budget | Tambahkan E22 sebelum meningkatkan worker |
| E03 ownership | Lease perlu terhubung dengan identitas kanonik dan generation | Gabungkan desain dengan E16/E30 |
| E04 task contract | Tambahkan policy persisten, compiled plan, dan output manifest | Hubungkan E17/E23/E24 |
| E05 completion evidence | Completed harus mempunyai bukti pada commit/artefak yang tepat, bukan sekadar exit atau state kosong | Pertahankan P0; integrasikan E16/E24 |
| E06 conflict-aware scheduling | Pencegahan konflik tidak otomatis mencegah agent memodifikasi clone milik agent lain | Tambahkan boundary E21 |
| E07 shared context | Context harus mereferensikan versi artefak dan input, bukan path ambigu | Hubungkan dengan E24 |
| E08 capability routing | Formula harus memvalidasi role target dan kemampuan runtime | E17 sebelum routing makin kompleks |
| E09 recovery | Checkpoint harus selamat dari GC; restart tidak boleh melupakan pause/no-push | Tambahkan E19/E23/E28 |
| E10 isolated tests | Isolasi checkout perlu mencakup port, database, toolchain, dan cache mutabel | Cocokkan dengan kebutuhan E25/E29 |
| E11 risk-based review | Formula review tersedia, tetapi diskusi #1753 menyebut pemanggilan masih manual | Naikkan prioritas wiring gate otomatis; keberadaan formula bukan bukti pipeline otomatis |
| E12 visual workflow | Output desain dan bukti visual perlu manifest serta versi input | Tambahkan E24; jangan menganggap visual review sudah dibuktikan issue audit |
| E13 dashboard | State harus fresh dan biaya pengamatan dibatasi | Gabungkan E16/E22; pisahkan dari observability aplikasi E26 |
| E14 routing evaluation | Belum ada benchmark audit yang membuktikan model routing terbaik | Baseline dahulu; adaptasi routing setelah telemetry valid |
| E15 Beads/Dolt | Adopsi existing saja tidak cukup untuk schema compatibility, backup, retention, dan migrasi | Perlu E19/E20/E28 |

Sumber koreksi review: [#1753](https://github.com/gastownhall/gastown/issues/1753). Ini memperjelas dokumen sebelumnya tanpa mengubah diam-diam klaim historisnya.

## Koreksi diagnosis yang penting

- **#4633:** diskusi mengoreksi klaim awal “tidak ada guard”. Ada guard role; masalah yang dibahas meliputi tidak adanya guard hook-state serta actor yang unset. Diagnosis awal provisioning juga direvisi: `CLAUDE.local.md` dilacak di repository proyek dan tersalin ke clone; pengecekan marker menghambat repair. Rekomendasi untuk kasus tersebut adalah memperbaiki tracking/ignore file sumber dan salinan, bukan mengasumsikan template internal selalu hardcode nama agent. [Diskusi](https://github.com/gastownhall/gastown/issues/4633).
- **#4032:** penulis menarik generalisasi bahwa `--set` selalu rusak. Scope yang lebih spesifik adalah perbedaan preview/live untuk `review_id`, rendering synthesis, serta tracking. Jangan membangun redesign berdasarkan diagnosis awal yang telah dikoreksi. [Diskusi](https://github.com/gastownhall/gastown/issues/4032).
- **#4727:** laporan membedakan perbaikan pada main dari paket release. “Open” tidak membuktikan belum ada fix, dan “ada fix di main” tidak membuktikan pengguna telah menerima fix. [Diskusi](https://github.com/gastownhall/gastown/issues/4727).
- **#4332:** permintaan runtime tambahan layak ditinjau, tetapi klaim di dalam diskusi tentang penghentian Gemini CLI tidak diverifikasi dalam audit ini dan tidak digunakan sebagai fakta. [Diskusi](https://github.com/gastownhall/gastown/issues/4332).

## Status 14 PR terkait

Status diambil dari metadata PR; `closed` berbeda dari `merged`. `merge_commit_sha` sendiri tidak membuktikan merge, karena dapat merujuk merge ref sintetis. Bahkan merged belum membuktikan masuk versi release yang dipakai.

| PR | Judul | Status terverifikasi |
| --- | --- | --- |
| [#4908](https://github.com/gastownhall/gastown/pull/4908) | fix(reaper): dry_run never rendered — gt reaper purge has always run live | OPEN — belum merged |
| [#4035](https://github.com/gastownhall/gastown/pull/4035) | fix: honor workflow step target + reconcile convoy tooling (#3798, #4032) | CLOSED — tidak merged |
| [#4701](https://github.com/gastownhall/gastown/pull/4701) | fix: queue mail notification instead of submitting a new turn | CLOSED — tidak merged |
| [#3923](https://github.com/gastownhall/gastown/pull/3923) | feat(tap-guard): cross-clone-block — block git -C writes against another crew clone | CLOSED — tidak merged |
| [#4135](https://github.com/gastownhall/gastown/pull/4135) | feat(migrate): town write-freeze for dolt migrations + harden freeze gate (gt-rb71t) | CLOSED — tidak merged |
| [#4068](https://github.com/gastownhall/gastown/pull/4068) | feat(dolt-backup): retention policy — prune .darc older than N days, keep floor of 3 | CLOSED — tidak merged |
| [#1226](https://github.com/gastownhall/gastown/pull/1226) | feat: make integration branches end-to-end across the pipeline | MERGED 2026-02-13T23:10:15Z |
| [#982](https://github.com/gastownhall/gastown/pull/982) | feat(rig): auto-detect tech stack and generate settings/config.json | CLOSED — tidak merged |
| [#4470](https://github.com/gastownhall/gastown/pull/4470) | Add policy-driven lifecycle artifact cleanup | CLOSED — tidak merged |
| [#3807](https://github.com/gastownhall/gastown/pull/3807) | fix(formula): unblock mol-idea-to-plan + multi-line --set truncation | CLOSED — tidak merged |
| [#394](https://github.com/gastownhall/gastown/pull/394) | feat(synthesis): record output location metadata on completed leg beads | CLOSED — tidak merged |
| [#406](https://github.com/gastownhall/gastown/pull/406) | feat(synthesis): record output location metadata on completed leg beads | CLOSED — tidak merged |
| [#4054](https://github.com/gastownhall/gastown/pull/4054) | fix: gt is not fork-aware at runtime: refinery start / crew context / no persistent refinery-off for rigs with upstream_url | CLOSED — tidak merged |
| [#4694](https://github.com/gastownhall/gastown/pull/4694) | fix: propagate convoy merge strategy to issue in executeSling (#4512) | CLOSED — tidak merged |

PR #4908 adalah kandidat workaround terbatas untuk masalah dry-run; bukan bukti compiler formula lengkap. PR #1226 yang merged memberi landasan integration branch/perintah konfigurabel; bukan bukti stack autodetection #983 selesai. PR closed lainnya sebaiknya dipelajari untuk ide dan alasan penutupan, bukan langsung di-cherry-pick sebagai solusi yang tervalidasi.

## Apa yang belum layak menjadi prioritas fork

| Issue/kelompok | Keputusan dan alasan |
| --- | --- |
| [#4851](https://github.com/gastownhall/gastown/issues/4851), [#4850](https://github.com/gastownhall/gastown/issues/4850), [#4580](https://github.com/gastownhall/gastown/issues/4580) | Isi menyebut Gas City/gc/gt-bd. Verifikasi applicability; lokasi issue di repo Gastown tidak otomatis membuktikan bug pada runtime Gastown |
| [#4707](https://github.com/gastownhall/gastown/issues/4707) | Probe; bukan requirement produk |
| [#1400](https://github.com/gastownhall/gastown/issues/1400), [#4298](https://github.com/gastownhall/gastown/issues/4298), [#1731](https://github.com/gastownhall/gastown/issues/1731) | Triage/docs/dependency housekeeping; tetap berguna untuk maintenance, tetapi bukan diferensiasi utama fork |
| [#4894](https://github.com/gastownhall/gastown/issues/4894), [#295](https://github.com/gastownhall/gastown/issues/295) | Newsletter dan estetika terminal: rendah terhadap correctness/throughput |
| [#517](https://github.com/gastownhall/gastown/issues/517), [#1248](https://github.com/gastownhall/gastown/issues/1248) | Ide morale/free-time dan ROMANCER2 memerlukan bukti manfaat; prioritaskan kontrak kerja, resolusi konflik, dan review terukur |
| [#622](https://github.com/gastownhall/gastown/issues/622) | TOON dapat dibenchmark terhadap biaya parsing, fidelity, dan token; tidak diasumsikan otomatis menghemat biaya |

## Urutan implementasi yang direkomendasikan

### Tahap 0 — baseline fork dan reproduksi

Pin kombinasi versi, pilih workload representatif, dan kumpulkan baseline 1/2/4/8 agent sesuai kapasitas mesin. Rekam accepted changes per jam, waktu tunggu, biaya per accepted change, rework, konflik, kegagalan recovery, dan kehilangan/duplikasi pekerjaan. “Accepted” berarti evidence serta gate pada commit yang tepat lulus. Hindari membandingkan jumlah task mentah dengan kompleksitas berbeda.

### Tahap 1 — keselamatan pekerjaan dan state

Mulai dari E20 + E16, lalu E03/E05 dan E19. Aktifkan E21/E23 sesuai operasi Git; E28 sebelum maintenance/migrasi dan E30 sebelum beberapa instance/coordinator. Gate: kegagalan baca tidak memicu cleanup; preservation gagal membatalkan delete; restart tidak menghilangkan kebijakan; worker lama tidak dapat mengklaim hasil untuk lease baru. Lakukan failure injection terarah pada batas-batas tersebut.

### Tahap 2 — orkestrasi yang dapat diandalkan

E18 + E17 + wiring otomatis E11, lalu E22/E02. Gate: retry pesan tidak menggandakan efek, preview dan eksekusi memakai plan yang sama, kegagalan satu rig terisolasi, dan review wajib tidak dapat dilompati melalui jalur alternatif yang didukung. Peningkatan concurrency hanya diterima jika throughput hasil valid meningkat tanpa memburuknya error/rework secara material.

### Tahap 3 — kapabilitas sesuai jenis proyek

E24 menjadi dasar synthesis dan traceability. Pilih E29 untuk stack yang digunakan, E25 untuk monorepo besar, E26 untuk debugging aplikasi, serta E27 bila beban incoming PR tinggi. E01/E08/E14 lebih bermanfaat setelah data biaya, keberhasilan, dan capability telah tepercaya. Jangan mengimplementasikan seluruh backlog sekaligus.

## Insight desain

- Banyak issue bukan kekurangan “kecerdasan agent”, melainkan kegagalan kontrak antarkomponen. Lebih banyak agent memperbesar peluang race dan ambiguitas tersebut.
- Outcome dan evidence perlu menjadi unit evaluasi. Task yang dianggap selesai namun harus diulang merupakan throughput semu.
- Satu enhancement dapat menyelesaikan satu keluarga issue; satu issue dapat memerlukan beberapa enhancement. Jangan memakai jumlah issue sebagai estimasi effort.
- Pisahkan **fork produk** yang memang mengembangkan branch sendiri dari **fork kontribusi upstream** yang menjaga branch pelacakan. Mode fork E23 perlu mendukung keduanya.
- Kedalaman implementasi diputuskan setelah reproduksi pada versi target; usulan dalam dokumen ini adalah backlog terarah, bukan jaminan diagnosis final.

## Sumber dan jejak audit

Inventaris berasal dari GitHub REST `GET /repos/gastownhall/gastown/issues?state=open&per_page=100` dengan pagination sampai akhir, kemudian item `pull_request` dikeluarkan. Pendalaman memakai body dan timeline issue; kandidat fix memakai metadata `/pulls/{number}`. Seluruh issue memiliki tautan langsung di inventaris. Pengambilan beberapa halaman bukan snapshot database atomik; angka adalah hasil pengambilan sesi audit, bukan janji total live selamanya.

File pendamping: `05-enhancement-tambahan.md` untuk spesifikasi backlog; `06-open-issue-inventory.md` untuk audit cakupan seluruh issue. Tiga dokumen awal E01–E15 tetap menjadi baseline, dengan koreksi dan prioritas tambahan dinyatakan eksplisit di sini.
