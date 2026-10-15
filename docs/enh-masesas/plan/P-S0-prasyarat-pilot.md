# Impl Plan P-S0 — Prasyarat Pilot

Spec: [`../spec/S0-prasyarat-pilot.md`](../spec/S0-prasyarat-pilot.md). Sebagian besar langkah operasional; satu paket kode (upgrade Beads).

**Status (14 Okt 2026):** fase 1 (k-posin, mirror lokal) selesai. Baseline manual (Langkah 7, Epic A) dipindah ke epic pertama S1c (keputusan Anda). Langkah 1–2 dan 4–8 dijalankan untuk fase 1; Langkah 3 dan konfigurasi `be`/`pro` di Langkah 4–6 berlaku untuk fase 2 (`oto-rent-*`). Hasil fase 1 ditandai **Hasil fase 1** di tiap langkah.

Konvensi: `$FORK` = checkout fork ini; `$TOWN` = direktori pilot town baru (usulan `~/gt-pilot`); `$BK` = direktori backup (usulan `~/gt-pilot-backup/<tanggal>`). Semua perintah yang mengubah data diberi tanda **[ubah]**.

## 0. Status yang sudah diketahui (dari verifikasi kode)

| Hal | Fakta |
| --- | --- |
| Versi Beads embed | `go.mod:18` `github.com/steveyegge/beads v1.0.5`, tanpa `replace` (sekarang v1.3.0, PR #1) |
| Pemakaian SDK | ±11 file non-test di `internal/{beads,convoy,daemon,mail,cmd,doltserver}`; mayoritas operasi lewat subprocess `bd` |
| Cek versi | `internal/deps/beads.go:19` minimum CLI 0.57.0; daemon (`internal/daemon/daemon.go:1143-1244`) **memblokir start** bila `bd_version` store lebih baru dari embed |
| Build | `make build` (ICU via `brew --prefix icu4c`); `make install` butuh `SKIP_UPDATE_CHECK=1` di fork, me-restart daemon bila jalan |
| Patrol | `$TOWN/mayor/daemon.json` → `patrols.<nama>.enabled`; kill switch kedua `disabled_patrols` di `$TOWN/settings/config.json`. `wisp_reaper` memuat auto-close stale 7 hari (tidak bisa dimatikan terpisah; opsi `dry_run`). `reaper.AutoClose` melewati label `gt:keep`, P0/P1, epic/convoy |
| Branch polecat | `polecat_branch_template` adalah properti rig (bukan `merge_queue`); variabel `{user} {year} {month} {name} {issue} {description} {timestamp}` |
| Default `integration_branch_template` | Komentar kode `integration/{epic}` vs docs `integration/{title}`: belum pasti, maka **di-set eksplisit**. `DetectIntegrationBranch` mencoba template terkonfigurasi lalu `integration/{epic}` |

## 1. Langkah

### Langkah 1 — Baseline fork (P0.1, S0-AC2)

1. `cd $FORK && make build` → catat output `./gt version`.
2. `go test ./... 2>&1 | tee $BK/../go-test-baseline.txt` (tanpa ubah apa pun).
3. Selidiki dua test `internal/beads` yang gagal: `go test ./internal/beads -run '<nama>' -v`. Klasifikasikan: environment (mis. butuh `bd`/Dolt versi tertentu) vs bug. Tulis hasil ke `../baseline/go-test-baseline.md`.
4. Bila bug: **[ubah]** buat bead perbaikan di `.beads` fork (setelah Anda setujui pembuatan bead).

**Hasil Langkah 1 (12 Okt 2026):** lihat `../baseline/go-test-baseline.md`. Tambahan: **[ubah]** `brew install tmux`, lalu ulangi `go test ./...` dengan `TMPDIR` non-symlink sebagai baseline pembanding (S0-AC11).

### Langkah 2 — Spike upgrade Beads (P0.1, D2-A, timebox 2 hari)

Branch: `chore/beads-upgrade`.

1. Tentukan target: versi modul `github.com/steveyegge/beads` yang sama dengan `bd version` di mesin (1.3.0). Cek tag tersedia: `go list -m -versions github.com/steveyegge/beads`.
2. **[ubah]** `go get github.com/steveyegge/beads@v1.3.0 && go mod tidy`.
3. `go build ./...` → perbaiki error kompilasi di ±11 file pemakai SDK. Prinsip: perubahan mekanis saja, tanpa refactor.
4. `go test ./...` → bandingkan dengan baseline Langkah 1. Regresi baru wajib diperbaiki atau dijelaskan. Kriteria tambahan: 12 test refinery kelompok C (`PROJECT IDENTITY MISMATCH`) hijau.
5. Test khusus: `go test ./internal/daemon -run Compatibility -v` (`daemon_compatibility_test.go`), tambahkan kasus: store `bd_version=1.3.0` + embed 1.3.0 → start diizinkan.
6. Review: agent `go-reviewer` atas diff.
7. Keputusan di akhir timebox:
   - Lulus → PR ke `main` fork (approval Anda), lalu `make build`.
   - Tidak lulus → fallback C (Langkah 2b), spike dilanjutkan paralel sebagai bead terpisah.

**Langkah 2b — Fallback C (bila perlu).** Pasang `bd` versi yang cocok dengan embed 1.0.5 di `$TOWN/.bin/bd`; wrapper `$TOWN/.bin/env.sh` yang menaruh `$TOWN/.bin` di depan PATH; semua sesi town di-launch dengan env itu. Catat di catatan kompatibilitas.

### Langkah 3 — Backup data per-repo (P0.2, S0-AC4, S0-AC5) — fase 2

Tidak dijalankan di fase 1: pilot memakai mirror lokal k-posin dan tidak membuka DB beads per-repo mana pun. Bukti isolasi fase 1 ada di Langkah 8.

1. Catat sebelum: untuk `oto-rent-be` dan `oto-rent-pro`: `bd count` (atau `bd list --all --json | jq length`) dan checksum direktori `.beads` (`find .beads -type f -not -name '*.lock' | sort | xargs shasum -a 256 | shasum -a 256`). Simpan ke `../baseline/data-before.md`.
2. **[ubah]** Backup: `dolt dump` per DB bila server per-repo jalan, plus salinan direktori `.beads` ke `$BK/<repo>/` (server dihentikan dulu atau salin dari dump).
3. Uji restore: salin `$BK/<repo>` ke direktori sementara, jalankan `bd count` di sana → harus sama.

### Langkah 4 — Pilot town (P0.2)

1. **[ubah]** `gt install $TOWN --git` (bukan di dalam repo mana pun).
2. **[ubah]** Sebelum daemon menyala, edit `$TOWN/mayor/daemon.json`: `patrols.wisp_reaper.dry_run=true` (atau `enabled=false`), `patrols.quota_dog.enabled=false`; tambahkan keduanya ke `disabled_patrols` bila memilih off.
3. **[ubah]** `gt rig add be <url oto-rent-be>` dan `gt rig add pro <url oto-rent-pro>` (D1-B: DB rig baru, kosong).
4. `gt daemon start` → S0-AC3. Bila diblokir versi: kembali ke Langkah 2/2b.

**Hasil fase 1.** Layout (`$PILOT` = `~/gt-pilot`):

| Path | Isi |
| --- | --- |
| `$PILOT/env.sh` | PATH tooling pilot di depan, `CLAUDE_CONFIG_DIR`, env Dolt/beads town (tanpa `BEADS_TEST_SERVER`) |
| `$PILOT/tools/bin/` | `gt` (salinan `make build`; ganti dengan `rm` lalu `cp`, karena menimpa binary di tempat membuat macOS mematikannya), `dolt` 2.3.5, `posin-gate`, wrapper `agy`/`agy-gt` |
| `$PILOT/mirror/k-posin.git` | Mirror bare k-posin, remote `origin` dihapus |
| `$PILOT/town` | `gt install --git --dolt-port 3417`; rig `posin` (prefix `kp`) dari `file://$PILOT/mirror/k-posin.git` |
| `$PILOT/claude-config` | Config Claude terkurasi + hook `pilot-guard.sh` (Bash) dan `pilot-file-guard.sh` (Write/Edit) |
| `$PILOT/agy-guard`, `$PILOT/agy-homes/aN` | Guard, seed izin, dan home per akun untuk `agy` (spike gt-af2e) |

Patrol di `mayor/daemon.json` sebelum daemon menyala: `dolt_backup`, `jsonl_git_backup`, `compactor_dog`, `scheduled_maintenance`, `doctor_dog`, `main_branch_test` mati; `wisp_reaper.dry_run=true`; `quota_dog` tidak dikonfigurasi (default mati). `gt up` dari kondisi mati: semua layanan start, rc=0 (setelah PR #17).

### Langkah 5 — Konfigurasi rig (P0.3)

`$TOWN/be/settings/config.json`:

```json
{
  "merge_queue": {
    "enabled": true,
    "run_tests": true,
    "test_command": "make ci-local",
    "setup_command": "<wrapper yang mengekspor CI_PG_PORT dari GT_POLECAT>",
    "merge_strategy": "pr",
    "vcs_provider": "github",
    "require_review": true,
    "integration_branch_template": "epic/{title}"
  },
  "role_agents": { "<role>": "claude" }
}
```

- `pro`: sama, dengan `test_command` = `fvm flutter analyze && fvm flutter test` dan `build_command`/`setup_command` untuk `build_runner` bila dipakai.
- Branch polecat: **[ubah]** `bd update gt-rig-<rig> --labels="polecat_branch_template:feat/gt-{issue}"` (atau file wisp `$TOWN/.beads-wisp/config/<rig>.json`).
- Mapping port: tulis wrapper kecil di repo BE (bukan di fork) yang menerjemahkan `GT_POLECAT` → `CI_PG_PORT`. Detail port mengikuti `scripts/ci-local.sh` BE.
- Akun Claude: daftarkan **hanya sebagai catatan** (tanpa `gt account add`, karena perintah itu membuat config dir; lihat S2b). Langkah ini dipindah ke S2b.
- **[ubah]** `gt crew add contract_owner --rig be` dan `--rig pro` → S0-AC8.

Catatan perubahan terhadap spec: pendaftaran akun dipindah ke S2b, karena `gt account add` yang sekarang melanggar I1. Spec S0 diperbarui.

**Hasil fase 1.** `$TOWN/posin/settings/config.json`, `merge_queue`:

```json
{
  "enabled": true,
  "merge_strategy": "direct",
  "run_tests": true,
  "setup_command": "$PILOT/tools/bin/posin-gate setup",
  "test_command": "$PILOT/tools/bin/posin-gate test",
  "integration_branch_template": "integration/{epic}",
  "integration_branch_auto_land": false
}
```

- `merge_strategy=direct` di sini berarti refinery yang merge (setelah gate). Ini bukan `gt sling --merge=direct`, yang sejak PR #18 ditolak di rig dengan merge queue.
- Crew: `gt crew add contract_owner --rig posin`. Nama `contract_owner` ditolak `gt` (tanda hubung dicadangkan).
- `gt sling` mewajibkan acceptance criteria berformat checklist (`- [ ] ...`) di bead (readiness S1a).
- Cost tier `economy` (`gt config cost-tier economy`): mayor/witness/refinery Sonnet, deacon/boot/dog Haiku, polecat/crew Opus.

### Langkah 6 — Uji jalur penuh (S0-AC6)

1. **[ubah]** Buat epic kecil uji di DB town per rig, satu bead `chore` sederhana di BE dan satu di mobile (deskripsi + AC lengkap).
2. `gt sling <bead> be` / `pro` → amati polecat → `gt done` → refinery → PR ke `epic/<title>`.
3. Catat temuan perilaku mode `pr` tanpa Actions → masukan S2a.

**Hasil fase 1.**

| Epic | Jalur | Hasil |
| --- | --- | --- |
| kp-2lp | 1 BE + 1 mobile, langsung ke `main` | Lulus dengan intervensi operator; memunculkan gt-zjlv, gt-dzck, gt-ka3d |
| kp-96c | 3 bead, `integration/kp-96c` → land `main` | Lulus tanpa intervensi; uji terarah gt-ka3d (MR ditahan ±10 m, 0 dispatch ulang) dan gt-dzck (nudge `immediate` saat Bash berjalan) |
| kp-dgt, kp-iwm, kp-7fb | polecat `agy` (spike gt-af2e) | kp-iwm memunculkan gt-pd1c.37; kp-7fb (setelah PR #17/#18) lulus tanpa intervensi, `--merge=direct` ditolak |

Mode `pr` belum diuji (mirror tanpa API PR); tetap untuk fase 2.

### Langkah 7 — Baseline metrik (P0.4, S0-AC9)

1. Pilih dua epic kecil berkompleksitas sebanding.
2. Epic A: workflow manual existing. Epic B: Gas Town tanpa enhancement.
3. Isi `../baseline/metrics-baseline.md` dengan tabel metrik `../11` § 3 (median + rentang, tanpa persen bila sampel kecil).

**Hasil fase 1.** Epic B (Gas Town tanpa S1c) tercatat di `../baseline/metrics-pilot-s0.md`. Epic A (manual) dipindah ke epic pertama S1c (P-S1c Langkah 6).

### Langkah 8 — Penutupan Tahap 0 (S0-AC5, S0-AC10)

1. Ulangi pengukuran Langkah 3.1 → `../baseline/data-after.md`; bandingkan.
2. Tulis `../baseline/compat.md`: commit fork, `bd version`, versi Beads embed, versi Dolt, `claude --version`.

**Hasil fase 1.** `compat.md` ditulis. Isolasi (pengganti Langkah 3.1 untuk fase 1): repo k-posin asli tetap di HEAD `4cfdde0` (titik awal mirror), tanpa branch `polecat/*` atau `integration/*`, dan commit pilot (mis. `c7d3b2e`) tidak ada di sana. Server Dolt project lain berjalan terus tanpa disentuh.

## 2. Pemetaan AC → bukti

| AC | Bukti fase 1 |
| --- | --- |
| S0-AC1 | `../baseline/compat.md` (fork `7eb40030`, Beads embed = `bd` = 1.3.0) |
| S0-AC2 | `../baseline/go-test-baseline.md` |
| S0-AC11 | tmux 3.8; suite hijau di macOS setelah PR #1, #5, #7, #10 |
| S0-AC3 | `gt up`/`gt daemon start` di pilot: semua layanan start, rc=0 |
| S0-AC4 | Mirror lokal tanpa remote; tidak ada perintah tulis ke repo asli atau DB per-repo (guard menolak path di luar pilot) |
| S0-AC5 | HEAD repo asli `4cfdde0` tetap, tanpa branch pilot (Langkah 8) |
| S0-AC6 | kp-96c dan kp-7fb: MR → gate → `integration/{epic}` → land `main` (Langkah 6) |
| S0-AC7 | `mayor/daemon.json` (Langkah 4) + default `quota_dog` mati di `internal/daemon/types.go` |
| S0-AC8 | Crew `contract_owner` di rig `posin` |
| S0-AC9 | `../baseline/metrics-pilot-s0.md`; baseline manual di epic pertama S1c |
| S0-AC10 | `../baseline/compat.md` |

## 3. Batasan eksplisit

- **Out of scope:** migrasi backlog, otomasi backup, perubahan kode selain upgrade Beads.
- **Known edge case:** `make install` me-restart daemon yang sedang jalan; gunakan `make build` + salin manual, atau `make safe-install`, saat town aktif.
- **Known limitation:** `wisp_reaper` mode `dry_run` juga menonaktifkan pembersihan wisp; ukuran DB town bisa tumbuh selama pilot (dipantau, bukan dimitigasi).

## 4. Temuan fase 1 dan perbaikannya

| Temuan | Dampak | Perbaikan |
| --- | --- | --- |
| Dialog trust Claude Code 2.1.296 tidak terbaca | Agent macet saat start | PR #12 (gt-swqb), #13 (gt-i81z, fail-closed) |
| `info/exclude` ditulis per-worktree, diabaikan git | `gt done` polecat terblokir | PR #14 (gt-zjlv) |
| Feeder convoy men-dispatch ulang bead yang MR-nya masih di queue | Polecat duplikat | PR #15 (gt-ka3d) |
| Nudge mengirim Escape saat tool berjalan / menjawab dialog | Gate refinery terputus | PR #16 (gt-dzck) |
| `gt up` cold start: cek safety stop kena circuit breaker bd; Usage tercetak untuk error runtime | Refinery tidak start | PR #17 (gt-pd1c.38) |
| `--merge=direct` (dipilih mayor) mendarat di `main`, mengabaikan integration branch dan gate | Kode tanpa gate di `main` | PR #18 (gt-pd1c.37) |
| Cek safety stop fail-open ("not found" apa pun = bead tidak ada) | Refinery bisa start tanpa cek | Terbuka: gt-pd1c.39 |
| Refinery melewati gate untuk MR pre-verified | Gate bergantung klaim polecat | Selesai: PR #20 (gt-pd1c.40, engine Go) + PR #27 (gt-pd1c.56, teks formula; pilot S1c membuktikan refinery LLM masih melewati gate sebelum #27) |
| Lain-lain kecil | — | Terbuka: gt-pd1c.33 (tmux tersisa setelah `gt down`), .34 (`prompt_flag` preset), .35 (`base_branch` ganda, hitungan merged), .36 (spam log daemon), .41 (SilenceUsage), .42 (cleanup land), gt-m67s (`lsof` tanpa `-a` di `gt down`) |

Perilaku operasional yang perlu diketahui:

- Mayor men-dispatch sendiri bead yang siap saat slot polecat kosong. Untuk bead yang didispatch operator, tulis larangan di deskripsi bead dan beri tahu mayor.
- `gt done` menutup bead saat MR diajukan bila semua AC tercentang; bila tidak, bead tetap `hooked` sampai refinery merge.
- Witness menyalakan refinery bila ada MR di queue; untuk menahan queue, matikan patrol refinery di daemon dan beri tahu witness.

## 5. Perlu tindakan Anda

- Menjalankan epic manual pembanding bersamaan dengan epic pertama S1c (P-S1c Langkah 6).
- Fase 2 (`oto-rent-*`): konfirmasi lokasi `$BK` dan jadwal setelah gate Tahap 1.
