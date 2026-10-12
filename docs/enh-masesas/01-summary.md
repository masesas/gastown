# Gas Town Fork — Ringkasan Riset dan Arah Enhancement

Tanggal penyusunan: 12 Oktober 2026 (Asia/Jakarta).

## Scope dokumen

Ringkasan keputusan, konteks kebutuhan, kemampuan existing, batas penelitian, dan referensi. Detail setiap enhancement berada di `02-enhancement-backlog.md`; strategi implementasi dan pengukuran berada di `03-insight-rekomendasi.md`.

## Ringkasan keputusan

Gas Town layak menjadi fondasi fork untuk orkestrasi banyak coding agent. Nilai enhancement terbesar terletak pada pengaturan kapasitas, kepemilikan pekerjaan, konsistensi konteks, dan bukti kelulusan hasil.

Target produktivitas adalah meningkatnya jumlah pekerjaan yang lolos verifikasi dan berhasil diintegrasikan. Jumlah agent aktif atau jumlah task yang ditutup tidak cukup untuk menunjukkan peningkatan produktivitas.

Lima prioritas pertama:

1. Pengelolaan akun lintas-provider dengan isolasi sesi.
2. Scheduler berdasarkan kapasitas akun dan sumber daya mesin.
3. Kepemilikan task atomik dan perlindungan dari hasil worker lama.
4. Kontrak task terstruktur dan acceptance criteria yang dapat diperiksa.
5. Bukti kelulusan yang terikat pada versi kode dan lingkungan pengujian.

## Konteks kebutuhan

- Beberapa akun Claude untuk koordinasi, perencanaan, dan pekerjaan kompleks.
- Beberapa akun Gemini atau lingkungan Antigravity untuk pekerjaan pengujian.
- Beberapa akun GPT melalui runtime yang sesuai untuk UI, implementasi, dan code review.
- Alur UI dimulai dari desain gambar, kemudian implementasi dan verifikasi visual.
- Beads dan Dolt existing perlu dipertahankan agar task serta riwayat tetap tersedia.
- Aturan proyek, skills, keputusan arsitektur, dan pengetahuan bersama perlu konsisten antar-agent.

Pembagian provider ini merupakan preferensi awal. Kecocokan setiap model/runtime tetap perlu dinilai pada task representatif codebase pengguna.

## Kemampuan existing yang diperiksa

| Area | Temuan audit | Arah enhancement |
| --- | --- | --- |
| Koordinasi | Mayor, Rig, Polecat, Convoy, dan persistensi pekerjaan tersedia | Pertahankan model dasar; batasi kewenangan koordinator |
| Runtime | Source memiliki preset Claude, Gemini CLI, dan Codex | Samakan kontrak lifecycle dan capability antar-runtime |
| Akun Claude | `gt account` mendaftarkan akun dengan direktori konfigurasi masing-masing | Perluas identitas dan kebijakan kapasitas lintas-provider |
| Rotasi akun | Source memiliki scanning sesi dan perencanaan rotasi | Integrasikan dengan scheduler serta checkpoint |
| Scheduler | Antrean, kapasitas global, batch dispatch, readiness, dan circuit breaker tersedia | Tambahkan kapasitas per akun dan jenis pekerjaan |
| Review | Formula review paralel berdasarkan aspek tersedia | Pilih reviewer sesuai risiko dan konsolidasikan temuan |
| Merge | Refinery memiliki quality gates dan fase post-squash | Kaitkan kelulusan dengan versi kode dan evidence |
| Data | Dokumentasi storage menempatkan Beads/Dolt sebagai penyimpan state | Pertahankan satu sumber status pekerjaan |
| Telemetry | Source memiliki telemetry dan korelasi run | Ukur throughput tervalidasi, rework, dan waktu tunggu |

Temuan di atas berasal dari audit source dan dokumentasi; bukan jaminan seluruh alur telah berhasil diuji pada mesin pengguna.

## Batas dan tingkat kepastian

- Snapshot source yang berhasil di-clone: `649b832b7672bc7a2dbef26f5983aba6198b819b`, dengan waktu commit 23 Juli 2026.
- Halaman web yang dibaca memuat aktivitas lebih baru. Snapshot tersebut tidak boleh dianggap sebagai upstream terbaru pada tanggal penyusunan.
- Penelitian mencakup pembacaan source, dokumentasi, dan sejumlah issue. Belum dilakukan benchmark atau reproduksi operasional multiakun.
- Issue komunitas digunakan sebagai petunjuk risiko. Status dan reproduksinya perlu diperiksa kembali sebelum dijadikan bug backlog.
- Daftar enhancement adalah rekomendasi desain. Tidak setiap item berarti upstream sama sekali belum memiliki mekanisme terkait.
- Dukungan Gemini CLI tidak membuktikan dukungan Antigravity. Integrasi Antigravity perlu validasi adapter tersendiri.
- Kemampuan coding agent tidak otomatis menyediakan image generation; pipeline UI perlu adapter pembuat gambar yang sesuai.

## Ketidaksesuaian dokumentasi dan source

Dokumen `model-aware-molecules.md` menyebut beberapa file `internal/models/*` selesai, tetapi direktori tersebut tidak ditemukan pada snapshot yang diperiksa. Sebaliknya, dokumen telemetry menyebut korelasi run belum masuk main, sementara implementasinya ditemukan pada source.

Implikasi: sebelum mengimplementasikan enhancement, periksa kode aktual, jalur pemanggilan, dan tes integrasi. Jangan menjadikan checklist desain sebagai satu-satunya bukti kesiapan fitur.

## Peta dokumen

| File | Scope | Penggunaan |
| --- | --- | --- |
| `01-summary.md` | Kesimpulan, kondisi existing, batas riset, sumber | Memahami arah fork dan dasar keputusan |
| `02-enhancement-backlog.md` | 15 enhancement dengan deskripsi, prioritas, impact, dependensi, dan acceptance criteria | Dasar penyusunan epic dan task Beads |
| `03-insight-rekomendasi.md` | Arsitektur, urutan implementasi, risiko, KPI, dan rekomendasi operasional | Menentukan pelaksanaan serta cara mengevaluasi hasil |

## Referensi penelitian

Tautan source dipasang pada commit yang diperiksa agar temuan dapat ditelusuri. Issue dan halaman utama dapat berubah.

| ID | Referensi | Relevansi |
| --- | --- | --- |
| S1 | [Repository Gas Town](https://github.com/gastownhall/gastown) | Konsep dan arsitektur umum |
| S2 | [Account management](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/cmd/account.go) | Multiakun Claude |
| S3 | [Quota rotation](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/quota/rotate.go) | Pemilihan akun pengganti |
| S4 | [Scheduler design](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/docs/design/scheduler.md) | Dispatch, readiness, dan antrean |
| S5 | [Scheduler configuration](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/scheduler/capacity/config.go) | Kapasitas global dan batch |
| S6 | [Scheduler pipeline](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/scheduler/capacity/pipeline.go) | Parameter dispatch dan filtering |
| S7 | [Refinery engineer](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/refinery/engineer.go) | Quality gates dan merge |
| S8 | [Code review formula](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/formula/formulas/code-review.formula.toml) | Review paralel |
| S9 | [Runtime presets](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/config/agents.go) | Claude, Gemini CLI, dan Codex |
| S10 | [Dolt storage design](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/docs/design/dolt-storage.md) | Database, routing, dan concurrency |
| S11 | [Model-aware molecules design](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/docs/design/model-aware-molecules.md) | Rencana model routing; perlu verifikasi implementasi |
| S12 | [Telemetry recorder](https://github.com/gastownhall/gastown/blob/649b832b7672bc7a2dbef26f5983aba6198b819b/internal/telemetry/recorder.go) | Event dan run correlation |
| S13 | [Issue #3905](https://github.com/gastownhall/gastown/issues/3905) | Laporan graceful retirement ketika kapasitas diturunkan |
| S14 | [Issue #4669](https://github.com/gastownhall/gastown/issues/4669) | Laporan deteksi hung berdasarkan umur sesi |
| S15 | [Issue #3740](https://github.com/gastownhall/gastown/issues/3740) | Riwayat hilangnya settings saat handoff; issue terlihat closed saat riset |

