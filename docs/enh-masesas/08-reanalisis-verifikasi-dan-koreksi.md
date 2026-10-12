# Re-analisis — Cakupan Verifikasi dan Koreksi atas Dokumen 01–06

Tanggal: 12 Oktober 2026. Legenda bukti: [V] terverifikasi, [U] laporan upstream, [I] inferensi, [A] asumsi (lihat `07`).

## 1. Cakupan pemeriksaan

### 1.1 Versi yang dianalisis

| Item | Nilai | Bukti |
| --- | --- | --- |
| Commit fork lokal | `649b832b7672bc7a2dbef26f5983aba6198b819b` (2026-07-23) | [V] `git rev-parse HEAD` |
| Upstream `gastownhall/gastown` `main` | **identik** dengan commit di atas (ahead 0, behind 0) | [V] `gh api .../compare/649b832b...main` |
| Release upstream terbaru | `v1.2.1` (2026-06-06), lebih tua dari `main` | [V] `gh release list` |
| Beads yang di-embed | `github.com/steveyegge/beads v1.0.5` | [V] `go.mod:18` |
| CLI `bd` lokal | `1.3.0` | [V] `bd version` |
| Dolt lokal | `1.83.3` | [V] `dolt version` |
| `gt` terpasang | **tidak ada** di PATH | [V] `which gt` |
| Go | 1.27.1 darwin/arm64 | [V] |

### 1.2 Yang dibaca dan dijalankan

- **Dokumen baseline.** Dibaca penuh: `01-summary.md` sampai `05-enhancement-tambahan.md`. `06-open-issue-inventory.md` hanya dibaca bagian definisi, ±40 baris indeks pertama, dan rekap. Tabel 375 baris tidak dibaca per baris.
- **Source Gas Town.** Diperiksa terarah dengan Read dan grep, oleh saya dan enam subagent verifikasi read-only. Area: sling/claim, `gt done`, daemon reaper, heartbeat, check-recovery, refinery (engine Go dan formula patrol), formula parser/validator, prime, convoy, scheduler capacity, account/quota, runtime presets, polecat manager/worktree, hooks/tap guards, deps/version check, rig add/adopt, dolt migrate/backup, dashboard fetcher.
- **Spot-check pribadi.** Klaim paling menentukan diperiksa ulang langsung:
  - `ProcessMRInfo` tanpa caller produksi.
  - `OnConflict`/`MaxRetryCount` tidak dipakai.
  - `prime.go:966` membatasi deskripsi ke 5 baris.
  - `queryAssignedBeads` menelan error.
  - Pesan blokir daemon di `daemon.go:1197`.
  - Gate APPROVED di formula refinery.
- **Build dan test.**
  - `go build ./...` gagal tanpa flag ICU. Error: `unicode/regex.h` not found dari `go-icu-regex`. Makefile sebenarnya sudah menangani ini (`Makefile:24-31`).
  - Dengan `CGO_CPPFLAGS/LDFLAGS` ICU, `go build ./cmd/gt` **berhasil**.
  - Test `internal/scheduler/capacity`, `internal/formula`, dan `internal/refinery` lulus. Hasil `internal/polecat` tidak terbaca karena output terpotong.
  - **2 test di `internal/beads` gagal lokal**: `TestGetRigDirForName`, `TestRunEnv_StripsPollutedDoltEnvAndUsesRigMetadata`. Penyebabnya belum diselidiki. Hipotesis [A]: perilaku `bd` 1.3.0 di PATH.
- **Issue upstream.** Status 30 issue yang dikutip dicek ulang via `gh issue view`. Semua OPEN kecuali #3740 (CLOSED/COMPLETED). Isi issue tidak direproduksi.
- **Project target.** Hanya dibaca, tanpa mengubah apa pun:
  - `oto-rent-be`: `CLAUDE.md`, `docs/rules/workflow.md`, `.beads/metadata.json`, `Makefile`, `scripts/ci-local.sh`, daftar branch dan workflow GitHub.
  - `oto-rent-pro`: `CLAUDE.md`, `.beads/metadata.json`, `.fvmrc`, potongan `docs/claude/workflows.md`, daftar branch.

### 1.3 Keterbatasan

- **Graph `codebase-memory-mcp` (diperbarui di Revisi 1).** Saat analisis awal, repo belum diindeks, sehingga temuan struktural berbasis grep + Read. Setelah izin Anda (Q9), repo diindeks pada 12 Oktober 2026. Klaim kunci diverifikasi ulang via graph dan semuanya terkonfirmasi (`12` § 4). Audit E01 dan primitif koordinasi di Revisi 1 memakai graph dengan cek coverage. Klaim negatif tetap dikonfirmasi dengan grep.
- **Belum ada eksekusi end-to-end Gas Town.** Tidak ada `gt install`, rig, atau polecat yang dijalankan. Perilaku runtime (mis. `gh pr checks --watch` tanpa GitHub Actions) masih [I]/[A].
- **Perilaku internal `bd` 1.3.0 tidak diperiksa.** Contohnya: apakah menulis `bd_version` ke metadata, dan apakah `bd blocked` me-resolve dependensi eksternal.
- **Project `oto-rent-pro` dan `oto-rent-be` tidak dianalisis secara arsitektural.** Hanya konvensi workflow, tracking, dan CI yang relevan dengan orkestrasi.

## 2. Matriks kemampuan existing (hasil verifikasi)

Kategori sesuai permintaan:

- **Tersedia & cukup** dikonfigurasi.
- **Tersedia, belum terhubung** ke workflow.
- **Perlu diperbaiki/diperkuat.**
- **Perlu dibangun.**

| Area | Kategori | Bukti utama |
| --- | --- | --- |
| Worktree per polecat; base dari `origin/<default>` atau integration branch; reuse pool | Tersedia & cukup | [V] `internal/polecat/manager.go:981-1009`, `:1746-1887` |
| Template nama integration branch dan polecat branch | Tersedia & cukup (perilaku perlu dicek di pilot) | [V] `config/types.go:1333` `integration_branch_template`; `rig/config.go:40` `polecat_branch_template` |
| Runtime per role/per rig (claude, gemini, codex, cursor, opencode, dll.) | Tersedia & cukup | [V] `config/agents.go:25-54`; resolusi `config/loader.go:1337-1351` |
| Akun Claude terisolasi per sesi (`CLAUDE_CONFIG_DIR`) | Tersedia & cukup | [V] `config/loader.go:883-923`, `config/env.go:178` |
| Rotasi kuota otomatis | Tersedia, belum terhubung dan **perlu diperbaiki**: `quota_dog` default off; rotasi menyalin token keychain; status limited tidak disimpan; Rotator swap config dir hanya dipakai test dan tidak dipakai karena memecah config (Revisi 2); `gt account add` membuat config dir hampir kosong; macOS & Claude saja | [V] `daemon/types.go:305-309`, `quota/keychain.go:85-178`, `cmd/quota.go:424-428`, `quota/executor.go:40-261` |
| Deteksi usage-limit di pane | Tersedia untuk semua sesi lewat `gt quota scan`; di jalur restart daemon hanya untuk Deacon | [V] `quota/scan.go:100-117`, `daemon/usage_limit.go:31-41`, `daemon.go:1665` |
| Ganti runtime saat handoff | Perlu dibangun (`agentOverride` ada tetapi selalu kosong) | [V] `cmd/handoff.go:1001`, `:260,292` |
| Briefing/review rencana multi-agent | Tersedia sebagai formula convoy (leg paralel + sintesis) dan `mol-idea-to-plan`; sintesis otomatis tidak terhubung; tidak ada protokol konsensus | [V] `design.formula.toml`, `mol-idea-to-plan.formula.toml:7-31`, `cmd/synthesis.go:692` |
| Gate approval manusia | Tipe bead `gate` ada; gate manusia tidak dikelola patrol; tanpa notifikasi | [V] `constants/constants.go:183-185`, `mol-deacon-patrol.formula.toml:345-371` |
| Wave berdasarkan dependensi | Tersedia (`gt convoy stage`/`launch`) | [V] `cmd/convoy_stage.go:990-1092`, `cmd/convoy_launch.go:158-190` |
| Akun non-Claude (Codex/Gemini) | Perlu dibangun | [V] `Account` hanya punya `ConfigDir` untuk `CLAUDE_CONFIG_DIR` (`config/types.go:1529-1542`) |
| Scheduler kapasitas | Tersedia (opt-in, `max_polecats=-1` default); tanpa pool per akun/resource | [V] `scheduler/capacity/config.go:12-21`, `pipeline.go:135` |
| Readiness scheduler terhadap blocker | Tersedia & fail-closed | [V] `cmd/capacity_dispatch.go:873-927` |
| Sling manual memeriksa dependensi | **Tidak** — hanya cek status | [V] `cmd/sling.go:616-660` |
| Convoy lintas-rig (track + feed) | Tersedia; feed **fail-open** saat lookup dependensi gagal | [V] `convoy/operations.go:202-218`, `:459-465` |
| Konteks task ke worker | Perlu diperkuat: deskripsi dipotong 5 baris; AC/design/notes tidak ditampilkan | [V] `cmd/prime.go:960-977` |
| Acceptance criteria | Hanya dicek sebagai jumlah `- [ ]` tersisa saat done/merge; bead tanpa AC lolos | [V] `beads/beads.go:306-318`, `cmd/done.go:456,476` |
| Readiness gate sebelum dispatch (deskripsi/AC kosong) | Perlu dibangun | [V] tidak ada di sling guards |
| Validasi variabel template formula | Tersedia, belum terhubung (`ValidateTemplateVariables` hanya dipanggil test) | [V] `formula/variable_validation.go:63-160` |
| Dry-run formula setara eksekusi | Perlu diperbaiki (3 jalur render berbeda) | [V] `cmd/sling_formula.go:364-381,463-489`, `cmd/prime_molecule.go:262-266` |
| Quality gate refinery (setup/typecheck/lint/build/test pada merge rehearsal) | Tersedia via formula LLM; tidak mekanis; bisa dilewati dengan penilaian "pre-existing" | [V] `mol-refinery-patrol.formula.toml:424-466,583-592`; config `cmd/prime_molecule.go:448-476` |
| Engine Go refinery (gates bernama, post-squash, fast-path `pre_verified`) | Tersedia, **belum terhubung** — hanya dipanggil test | [V] spot-check: `ProcessMRInfo` tanpa caller non-test |
| `on_conflict`, `max_retry_count` | Perlu diperbaiki — di-parse tetapi tidak pernah dipakai | [V] `refinery/engineer.go:107,155,372` |
| Evidence terikat SHA | Perlu dibangun (MR hanya menyimpan `commit_sha` dan klaim `pre_verified`) | [V] `cmd/done.go:1725,1744-1751` |
| Merge via PR GitHub (`merge_strategy=pr`) | Tersedia; selalu mensyaratkan `APPROVED` walau `require_review=false` (inkonsisten) | [V] `mol-refinery-patrol.formula.toml:70,750-790` |
| Review otomatis sebelum merge | Tersedia, belum terhubung (#1753); `quality-review` default off dan non-gating | [V]/[U] |
| Resolusi konflik merge | Tersedia (task "Resolve merge conflicts" + dependensi MR) | [V] `refinery/engineer.go:1640-1679` |
| Deteksi overlap scope sebelum dispatch | Perlu dibangun | [V] grep: tidak ada; bead tak punya field scope |
| Claim task | Lock file per bead (lokal host), lalu read-then-write; tanpa lease/fencing | [V] `cmd/sling.go:608,1290-1306`, `sling_helpers.go:1278` |
| `gt done` memvalidasi kepemilikan | Perlu dibangun; lookup assignment menelan error | [V] `cmd/done.go:2570-2588` |
| Liveness/reaper | Perlu diperbaiki: heartbeat tanpa session generation; error baca → "tidak ada pekerjaan" | [V] `session/heartbeat.go:33-38`, `daemon/daemon.go:2827-2849,2906-2932` |
| Keputusan nuke (`SAFE_TO_NUKE`) | Sebagian besar fail-closed; celah pada hook kosong/tak terbaca | [V] `polecat/workstate.go:116-174`, `cmd/polecat.go:1078,1346` |
| Isolasi tes per polecat | Tersedia di level env (`GT_POLECAT`, `GT_RIG`); tanpa alokator | [V] `config/env.go:128-138` |
| Batas tulis antar-workspace | Perlu dibangun; hook hanya pola Bash tertentu | [V] `hooks/config.go:1017-1061` |
| Cek kompatibilitas versi | Daemon memblokir jika store lebih baru; `gt doctor` hanya cek versi minimum bd | [V] `daemon/daemon.go:1143-1244`, `deps/beads.go:19` |
| Adopsi `.beads` existing | `rig add` membuat DB baru kosong di Dolt town; `dolt migrate` **memindahkan**, bukan menyalin; `rig add` tanpa dry-run | [V] `rig/manager.go:577-695`, `doltserver.go:2862-2948` |
| Backup Dolt | Patrol butuh remote `<db>-backup` yang dibuat manual; restore tidak diuji | [V] `daemon/dolt_backup.go:190-235` |
| Model routing per step (`internal/models/*`) | Tidak ada; dokumen desain usang | [V] glob kosong; `docs/design/model-aware-molecules.md` |

## 3. Fakta dari project target yang memengaruhi desain

| Fakta | Bukti | Dampak |
| --- | --- | --- |
| `oto-rent-be`: Spring Boot 3 / Java 21, Gradle multi-module (`app`, `domain`, `core`, `feature/*`) | [V] `CLAUDE.md` | Area konflik: `shared/`, `core/`, `sql/migration/`, dependency Gradle |
| `oto-rent-pro`: Flutter (FVM 3.38.2), clean architecture, `build_runner` | [V] `CLAUDE.md`, `.fvmrc` | Generated file (`*.g.dart`) dan `app_router.dart` rawan konflik |
| Kedua repo memakai bd + Dolt **server per-repo** (port 13469/13715, DB `oto_rent_be` / `oto_rent_host_app`) | [V] `.beads/metadata.json`, `dolt-server-config.yaml` | Bertentangan dengan model satu Dolt server per town (`docs/design/dolt-storage.md`) |
| Workflow epic existing: `epic/<nama>` → child `<type>/<desc>` → PR ke epic; `ci/<epic>-wave<n>`; branch `ai/*` & `epic/*` tidak pernah dihapus | [V] `oto-rent-be/docs/rules/workflow.md:21-37` | Gas Town integration branch dan polecat branch harus dipetakan ke konvensi ini |
| GitHub Actions dimatikan; CI berjalan lokal via `make ci-local` yang memposting status `local-ci` ke SHA yang diuji; working tree wajib bersih; Postgres per `CI_PG_PORT` | [V] `Makefile:343`, `scripts/ci-local.sh:9-59`, `workflow.md:32` | Evidence terikat SHA **sudah ada** di level project; `gh pr checks --watch` di mode `pr` kemungkinan tidak menemukan check Actions [I] |
| Main checkout adalah ruang kerja manual user; agent wajib di worktree | [V] `CLAUDE.md` | Selaras dengan worktree polecat; town Gas Town harus memakai clone terpisah |
| Policy bd: setiap issue wajib checklist + acceptance criteria + `--actor` | [V] `CLAUDE.md` | Data AC sudah tersedia untuk readiness gate (Rank 1) |
| Mobile: test hanya repository unit test + cubit test, dengan konfirmasi user | [V] `CLAUDE.md` oto-rent-pro | Gate mobile = analyze + test existing; agent tidak boleh menambah test tanpa izin, dan ini harus masuk kontrak task |

## 4. Koreksi atas dokumen sebelumnya

Dokumen asli tidak diubah. Kolom "Status" menyatakan klaim lama: **dikoreksi**, **dipertajam**, atau **dikonfirmasi**.

| # | Dokumen / klaim lama | Status | Koreksi |
| --- | --- | --- | --- |
| K1 | 01: "Snapshot tidak boleh dianggap upstream terbaru" | Dikoreksi | [V] Snapshot **adalah** `main` upstream terkini. Upstream tidak aktif di `main` sejak 23 Juli 2026. Risiko sesungguhnya adalah stagnasi upstream, bukan ketertinggalan snapshot. |
| K2 | 01/02/03: Lima prioritas pertama = E01–E05, dengan E01 (akun lintas-provider) dan E02 (scheduler per akun) sebagai P0 | Dikoreksi | [V] Runtime per role/rig dan akun Claude per sesi sudah ada. Kebutuhan multi-provider belum terbukti menjadi bottleneck. E01/E02 diturunkan ke "tunda dengan pemicu" (`11` § 2). |
| K3 | 02 E05: "Gunakan kembali pipeline refinery, termasuk post-squash verification" | Dikoreksi | [V] Post-squash gates hanya ada di engine Go yang **tidak terhubung**. Refinery produksi adalah LLM yang mengikuti formula. Reuse berarti wiring dan perubahan formula, bukan konfigurasi. |
| K4 | 02 E11: "Gunakan formula review existing" | Dipertajam | [V] Formula review tidak terhubung. `quality-review` default off dan eksplisit non-gating. E11 bergantung pada mekanisme gate Rank 2. |
| K5 | 02 E10: "Untuk Spring Boot dan mobile, mulai dari database per run" | Dikoreksi | [V] `ci-local.sh` sudah mengisolasi Postgres per port. Yang kurang hanya pemetaan `GT_POLECAT` → `CI_PG_PORT`, yang cukup lewat konfigurasi. Test Flutter yang diizinkan tidak memakai DB [I]. |
| K6 | 02 E15: adopsi Beads/Dolt sebagai P2 kecuali jadi prasyarat | Dipertajam | [V] Ini **pasti** prasyarat. `rig add` membuat DB kosong; `dolt migrate` memindahkan data (bukan copy); dan ada version skew. Diangkat ke P0.1/P0.2. |
| K7 | 04/05: E16–E23, E28, E30 mayoritas P0 | Dikoreksi | Terlalu banyak P0 membuat prioritas kehilangan makna. Untuk satu town dengan 2–4 polecat, hanya E16 (slice) dan E20 (minimal) yang P0. E18/E21/E22/E28/E30 menjadi hardening bersyarat. |
| K8 | 05 E17: compiler formula dengan plan hash bersama untuk preview dan eksekusi | Dipertajam | [V] Langkah termurah dengan manfaat terbesar: hubungkan `ValidateTemplateVariables` yang sudah ada saat sling. Compiler penuh ditunda. |
| K9 | 03 § 3: penempatan peran per provider (Gemini test author, Codex UI, dst.) | Dipertajam | Masih preferensi tanpa bukti. Mulai dari satu runtime; jadikan routing eksperimen setelah baseline. Test runner tetap skrip deterministik, sesuai 03. |
| K10 | 01: ketidaksesuaian `model-aware-molecules.md` | Dikonfirmasi | [V] `internal/models/*` tidak ada. Penggantinya `RoleAgents`, `CostTier`, dan `agent` per leg convoy. |
| K11 | 02 E03: "Pertahankan mekanisme locking existing" | Dipertajam | [V] Mekanisme yang ada adalah file lock per bead di satu host, efektif untuk satu town. Lease/fencing penuh hanya perlu untuk multi-host atau force re-sling yang sering. Pengecekan kepemilikan di `gt done` masuk Rank 5 (Revisi 1; sebelumnya Rank 3). |
| K12 | 04: "Banyak laporan bisa berasal dari incompatibility yang sama" | Dikonfirmasi lokal | [V] Kombinasi `bd 1.3.0` + embed `1.0.5` terjadi di mesin Anda. Perilaku #4789 belum direproduksi. |
| K13 | 02 E12, 01: pipeline desain/Antigravity | Tetap di luar scope | Tidak ada bukti baru. Tetap ditunda. |
| K14 | 06: inventaris 375 issue sebagai dasar prioritas | Dipertajam | Inventaris berguna untuk navigasi, tetapi jumlah issue bukan ukuran nilai. Prioritas di 09 berbasis gap terverifikasi dan workflow Anda. |

### Koreksi Revisi 1 (atas dokumen 07–11 versi awal)

| No | Klaim versi awal | Status | Dasar |
| --- | --- | --- | --- |
| K15 | 07/09/K2: E01 ditunda sampai kuota terbukti bottleneck | Dibalik oleh keputusan Anda | E01 menjadi Rank 4, dengan scope dipersempit ke failover akun (4a) dan fallback provider (4b). Prioritas ini berbasis keputusan bisnis, bukan baseline; frekuensi limit tetap diukur |
| K16 | 07 #6: "Multi-akun tidak seburuk anggapan 01/02" | Dikoreksi sebagian | [V] Mesin rotasi lebih lengkap dari yang ditulis, tetapi mekanismenya menyalin token antar-keychain, tidak menyimpan status limited, dan Rotator yang lebih aman tidak terhubung (`12` § 3.1) |
| K17 | 09 Rank 5: E06 deteksi overlap sebagai mekanisme pencegahan konflik | Diganti | Usulan Anda (brief + contract owner + konsumen terblokir) mencegah konflik lebih langsung. E06 ditunda dengan pemicu (`11` § 2) |
| K18 | 09 Rank 4: E31 cukup sebagai guard dependensi + pin kontrak | Diperluas | Menjadi Rank 3 (E31+) dengan bagian prosedural (overlay formula) dan bagian kode |
| K19 | 09 Rank 2: kemungkinan perlu gate dua tingkat | Dihapus | `make ci-local` ±4 menit (Q4) |
| K20 | 09 Rank 3 (E16) poin (d) usage-limit polecat | Dipindah | Ke Rank 4a, agar deteksi limit dan tindakan rotasi berada di satu enhancement |
| K21–K24 | Revisi 1: Rotator swap config dir; `gt account add`; `account switch`; rotasi keychain | Dibatalkan/dipertajam (Revisi 2) | Lihat `13` § 2 |
| K25 | Revisi 1: Anda meng-approve brief | Diganti (Revisi 3) | Lihat `14` § 6 |
