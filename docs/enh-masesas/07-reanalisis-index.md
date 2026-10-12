# Re-analisis Kritis Enhancement Gas Town — Indeks dan Executive Summary

Tanggal: 12 Oktober 2026 (Asia/Jakarta). Status: **analisis dan rekomendasi**. Dokumen ini tidak mengubah source code, task, atau issue.

**Revisi 1 (12 Oktober 2026):** jawaban Q1/Q3/Q4/Q9, keputusan Anda menjadikan E01 prioritas, dan usulan briefing + contract owner sudah dimasukkan. Ringkasan perubahan dan evaluasi usulan ada di `12-reanalisis-revisi-1.md`.

**Revisi 2 (12 Oktober 2026):** desain E01 diubah agar semua akun memakai satu config global per runtime, dan selaras dengan claude-swap yang sudah Anda pakai. Rekomendasi Revisi 1 (swap config dir per akun) dibatalkan. Detail di `13-reanalisis-revisi-2-e01-config-tunggal.md`.

**Revisi 3 (12 Oktober 2026):** brief disetujui Mayor. Hal krusial dikumpulkan sebagai pertanyaan untuk Anda, pekerjaan berbasis asumsi diberi tanda, dan rework dilakukan berdasarkan tanda. PR epic → master diblokir selama ada asumsi terbuka. Detail di `14-reanalisis-revisi-3-approval-mayor-dan-asumsi.md`.

Dokumen 01–06 tetap menjadi baseline. Re-analisis ini memperlakukan isi 01–06 sebagai hipotesis. Setiap koreksi dicatat eksplisit di `08-reanalisis-verifikasi-dan-koreksi.md` dan tidak menimpa dokumen lama.

## Peta dokumen re-analisis

| File | Isi | Urutan format yang diminta |
| --- | --- | --- |
| `07-reanalisis-index.md` (file ini) | Executive summary, rekomendasi utama, legenda bukti | 1 |
| `08-reanalisis-verifikasi-dan-koreksi.md` | Cakupan pemeriksaan, fakta terverifikasi, matriks kemampuan existing, koreksi atas 01–06 | 2 |
| `09-reanalisis-top-enhancement.md` | Prasyarat tahap 0, tabel kandidat, ranking, detail 5 enhancement terpilih | 3, 4 |
| `10-reanalisis-keputusan-dan-workflow.md` | Keputusan arsitektur, workflow operasional, kontrak data untuk UI kelak | 5, 6 |
| `11-reanalisis-roadmap-dan-pertanyaan.md` | Urutan implementasi dan gate per tahap, enhancement yang ditunda, pertanyaan konfirmasi | 7, 8, 9 |
| `12-reanalisis-revisi-1.md` | Log Revisi 1, evaluasi usulan briefing/contract owner, temuan E01, verifikasi graph, pertanyaan Q10–Q11 | — |
| `13-reanalisis-revisi-2-e01-config-tunggal.md` | Revisi 2: invariant config tunggal, analisis claude-swap, opsi akun bersamaan, scope 4a/4b baru, Q12 | — |
| `14-reanalisis-revisi-3-approval-mayor-dan-asumsi.md` | Revisi 3: approval brief oleh Mayor, protokol pertanyaan krusial + tanda asumsi + rework | — |

## Legenda tingkat bukti

Dipakai di seluruh dokumen 07–11:

- **[V] Terverifikasi**: dibaca langsung di source, konfigurasi, atau hasil perintah pada sesi ini, dengan `file:line`.
- **[U] Laporan upstream**: isi issue atau PR GitHub. Status open/closed sudah dicek ulang, tetapi tidak direproduksi.
- **[I] Inferensi**: kesimpulan dari kode yang dibaca, tanpa eksekusi atau reproduksi.
- **[A] Asumsi**: belum dicek. Perlu konfirmasi atau validasi pilot.

## 1. Executive summary

### Temuan yang paling mengubah arah

1. **[V] Upstream praktis berhenti di snapshot yang dianalisis.** `main` di `gastownhall/gastown` identik dengan `649b832b` (compare: `identical`, ahead 0 / behind 0). Commit terakhirnya bertanggal 23 Juli 2026. Release terakhir adalah `v1.2.1` (6 Juni 2026), lebih tua dari `main`. Seluruh issue kunci yang dikutip 04/05 masih OPEN.
   - Implikasi: fork ini **tidak bisa mengandalkan perbaikan upstream**. Setiap enhancement menjadi beban maintenance Anda sendiri. Karena itu jumlah perubahan core harus sesedikit mungkin.
2. **[V] Lingkungan lokal belum siap menjalankan Gas Town di data Beads yang sudah ada.**
   - Binary `gt` belum terpasang.
   - `go.mod` menyematkan `beads v1.0.5`, sedangkan CLI `bd` lokal versi `1.3.0`.
   - Daemon `gt` menolak start bila store punya `bd_version` lebih baru dari Beads yang di-embed (`internal/daemon/daemon.go:1197`).
   - `gt rig add` membuat **database baru yang kosong** di Dolt server town. Perintah ini tidak mengimpor issue yang sudah ada.
   - Kedua project Anda memakai Dolt server per-repo, masing-masing di port `13469` dan `13715`.
   - Kesimpulan: penyelarasan versi dan strategi data adalah **prasyarat**, bukan enhancement.
3. **[V] Quality gate dan review di refinery dijalankan oleh LLM yang mengikuti teks formula, bukan oleh mekanisme.**
   - Engine Go refinery (`ProcessMRInfo`, `runGates`, post-squash gates) **hanya dipanggil dari test**.
   - Refinery produksi adalah agent yang mengikuti `mol-refinery-patrol.formula.toml`. Agent itu boleh menilai kegagalan sebagai "pre-existing" lalu tetap merge.
   - Tidak ada evidence gate yang terikat ke SHA.
   - Formula `code-review` tidak terhubung ke pipeline.
   - Ini adalah gap correctness terbesar yang relevan dengan tujuan "pekerjaan valid yang benar-benar selesai".
4. **[V] Kontrak task tidak sampai utuh ke worker.**
   - `gt prime` hanya menampilkan 5 baris pertama deskripsi (`internal/cmd/prime.go:966`).
   - Field `acceptance_criteria`, `design`, dan `notes` tidak ditampilkan.
   - Tidak ada readiness check sebelum dispatch.
   - Padahal policy bd di project Anda sudah mewajibkan acceptance criteria. Gas Town belum memanfaatkannya.
5. **[V] Project Anda sudah punya sebagian solusi yang dianggap "perlu dibangun" oleh dokumen 02.**
   - `oto-rent-be/scripts/ci-local.sh` mewajibkan working tree bersih.
   - Script itu memposting commit status `local-ci` ke SHA yang diuji.
   - Script itu mengisolasi Postgres per port (`CI_PG_PORT`) untuk run paralel antar-worktree.
   - Gas Town cukup **menghubungkan** mekanisme ini, tidak perlu membangun ulang.
6. **[V] Multi-akun dan multi-runtime: mesinnya ada, tetapi rotasinya perlu diperbaiki sebelum dipakai** (dikoreksi di Revisi 1).
   - Runtime per role dan per rig sudah bisa dikonfigurasi (`RoleAgents`, 13 preset termasuk claude/gemini/codex).
   - Akun Claude diisolasi per sesi lewat `CLAUDE_CONFIG_DIR`. `gt account` dan `gt quota scan|rotate` sudah ada.
   - Rotasi bawaan menyalin token OAuth ke entri keychain default, tidak menyimpan status limited, dan hanya mendukung Claude. `gt account add` membuat config dir yang hampir kosong, sehingga skills, rules, plugins, dan history tidak ikut (Revisi 2).
   - Atas keputusan Anda, E01 menjadi **Rank 4** dengan scope dipersempit (`12` § 3).
7. **[V] Primitif untuk koordinasi contract-first sudah tersedia sebagian** (Revisi 1). Gas Town punya formula review paralel + sintesis, `gt convoy stage` berbasis wave, bead `gate`, dan crew bernama. Celahnya ada di sling manual yang melewati dependensi, feed yang fail-open, dan gate manusia tanpa pemilik. Usulan briefing Anda bisa dijalankan secara prosedural sejak pilot, lalu diperkuat dengan kode.

### Rekomendasi utama

Kerjakan **Tahap 0 (prasyarat)**, lalu **5 enhancement** berikut. Detail ada di `09`.

| Rank | Enhancement | ID asal | Sifat pekerjaan |
| --- | --- | --- | --- |
| 1 | Kontrak task utuh sampai ke worker + readiness gate | E04 (+ sebagian E17) | Memperkuat yang ada; effort kecil |
| 2 | Gate merge mekanis berbasis evidence terikat SHA | E05 | Menghubungkan yang ada + membangun verifier kecil; gate satu tingkat (`ci-local` ±4 menit) |
| 3 | Koordinasi contract-first: brief, contract owner, guard dependensi, pin kontrak | **E31+** (usulan Anda + E31; menggantikan E06) | Prosedural dulu (overlay formula), lalu kode fork |
| 4 | Akun Claude bersamaan dengan satu config global (4a) + fallback provider (4b) | **E01 slice** (keputusan Anda; Revisi 2) | Launcher token per proses + wiring deteksi limit; 4b diawali spike per provider |
| 5 | State fail-closed di boundary lifecycle (reaper, done, heartbeat) | E16 (slice) | Perbaikan implementasi existing |

Prasyarat Tahap 0 (detail di `09` § 2):

- **P0.1** Penyelarasan toolchain (E20 minimal).
- **P0.2** Pilot town terisolasi dengan backup data (E15/E19 minimal).
- **P0.3** Konfigurasi rig untuk stack Anda (E29 via config, merge `pr`, template branch).
- **P0.4** Baseline pengukuran.

### Yang sengaja tidak diprioritaskan sekarang

- **Scheduler per akun/provider/resource (E02)**: failover Rank 4 menangani akun yang limit. Pembagian beban proaktif baru dibutuhkan bila rotasi sering terjadi.
- **Deteksi overlap otomatis (E06)**: peta kepemilikan di Rank 3 mengambil peran utamanya.
- **Pesan durable (E18)**, **split-brain (E30)**, dan **control-plane load (E22)**: risikonya nyata pada skala besar, tetapi kecil untuk satu town dengan 2–4 polecat.
- **UI/dashboard (E13)**: cukup siapkan kontrak event (`10` § 3).

Kondisi kapan item-item ini layak naik prioritas ada di `11` § 2.

### Tidak ada klaim angka produktivitas

Tidak ada benchmark yang dijalankan. Semua dampak bersifat kualitatif. Target numerik ditetapkan setelah baseline P0.4.
