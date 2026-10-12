# Impl Plan P-S0 — Prasyarat Pilot

Spec: [`../spec/S0-prasyarat-pilot.md`](../spec/S0-prasyarat-pilot.md). Sebagian besar langkah operasional; satu paket kode (upgrade Beads).

Konvensi: `$FORK` = checkout fork ini; `$TOWN` = direktori pilot town baru (usulan `~/gt-pilot`); `$BK` = direktori backup (usulan `~/gt-pilot-backup/<tanggal>`). Semua perintah yang mengubah data diberi tanda **[ubah]**.

## 0. Status yang sudah diketahui (dari verifikasi kode)

| Hal | Fakta |
| --- | --- |
| Versi Beads embed | `go.mod:18` `github.com/steveyegge/beads v1.0.5`, tanpa `replace` |
| Pemakaian SDK | ±11 file non-test di `internal/{beads,convoy,daemon,mail,cmd,doltserver}`; mayoritas operasi lewat subprocess `bd` |
| Cek versi | `internal/deps/beads.go:19` minimum CLI 0.57.0; daemon (`internal/daemon/daemon.go:1143-1244`) **memblokir start** bila `bd_version` store lebih baru dari embed |
| Build | `make build` (ICU via `brew --prefix icu4c`); `make install` butuh `SKIP_UPDATE_CHECK=1` di fork, me-restart daemon bila jalan |
| Patrol | `$TOWN/mayor/daemon.json` → `patrols.<nama>.enabled`; kill switch kedua `disabled_patrols` di `$TOWN/settings/config.json`. `wisp_reaper` memuat auto-close stale 7 hari (tidak bisa dimatikan terpisah; opsi `dry_run`). `reaper.AutoClose` melewati label `gt:keep`, P0/P1, epic/convoy |
| Branch polecat | `polecat_branch_template` adalah properti rig (bukan `merge_queue`); variabel `{user} {year} {month} {name} {issue} {description} {timestamp}` |
| Default `integration_branch_template` | Komentar kode `integration/{epic}` vs docs `integration/{title}`: belum pasti, maka **di-set eksplisit** |

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

### Langkah 3 — Backup data per-repo (P0.2, S0-AC4, S0-AC5)

1. Catat sebelum: untuk `oto-rent-be` dan `oto-rent-pro`: `bd count` (atau `bd list --all --json | jq length`) dan checksum direktori `.beads` (`find .beads -type f -not -name '*.lock' | sort | xargs shasum -a 256 | shasum -a 256`). Simpan ke `../baseline/data-before.md`.
2. **[ubah]** Backup: `dolt dump` per DB bila server per-repo jalan, plus salinan direktori `.beads` ke `$BK/<repo>/` (server dihentikan dulu atau salin dari dump).
3. Uji restore: salin `$BK/<repo>` ke direktori sementara, jalankan `bd count` di sana → harus sama.

### Langkah 4 — Pilot town (P0.2)

1. **[ubah]** `gt install $TOWN --git` (bukan di dalam repo mana pun).
2. **[ubah]** Sebelum daemon menyala, edit `$TOWN/mayor/daemon.json`: `patrols.wisp_reaper.dry_run=true` (atau `enabled=false`), `patrols.quota_dog.enabled=false`; tambahkan keduanya ke `disabled_patrols` bila memilih off.
3. **[ubah]** `gt rig add be <url oto-rent-be>` dan `gt rig add pro <url oto-rent-pro>` (D1-B: DB rig baru, kosong).
4. `gt daemon start` → S0-AC3. Bila diblokir versi: kembali ke Langkah 2/2b.

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
- **[ubah]** `gt crew add contract-owner --rig be` dan `--rig pro` → S0-AC8.

Catatan perubahan terhadap spec: pendaftaran akun dipindah ke S2b, karena `gt account add` yang sekarang melanggar I1. Spec S0 diperbarui.

### Langkah 6 — Uji jalur penuh (S0-AC6)

1. **[ubah]** Buat epic kecil uji di DB town per rig, satu bead `chore` sederhana di BE dan satu di mobile (deskripsi + AC lengkap).
2. `gt sling <bead> be` / `pro` → amati polecat → `gt done` → refinery → PR ke `epic/<title>`.
3. Catat temuan perilaku mode `pr` tanpa Actions → masukan S2a.

### Langkah 7 — Baseline metrik (P0.4, S0-AC9)

1. Pilih dua epic kecil berkompleksitas sebanding.
2. Epic A: workflow manual existing. Epic B: Gas Town tanpa enhancement.
3. Isi `../baseline/metrics-baseline.md` dengan tabel metrik `../11` § 3 (median + rentang, tanpa persen bila sampel kecil).

### Langkah 8 — Penutupan Tahap 0 (S0-AC5, S0-AC10)

1. Ulangi pengukuran Langkah 3.1 → `../baseline/data-after.md`; bandingkan.
2. Tulis `../baseline/compat.md`: commit fork, `bd version`, versi Beads embed, versi Dolt, `claude --version`.

## 2. Pemetaan AC → bukti

| AC | Bukti |
| --- | --- |
| S0-AC1 | `gt version`, `go list -m github.com/steveyegge/beads`, `bd version` di `compat.md` |
| S0-AC2 | `go-test-baseline.md` |
| S0-AC3 | Output `gt daemon start` + `gt daemon status` |
| S0-AC4 | Log uji restore |
| S0-AC5 | `data-before.md` vs `data-after.md` |
| S0-AC6 | URL PR BE dan mobile |
| S0-AC7 | Kutipan `daemon.json` + `settings/config.json` |
| S0-AC8 | `gt crew list --rig be/pro` |
| S0-AC9 | `metrics-baseline.md` |
| S0-AC10 | `compat.md` |

## 3. Batasan eksplisit

- **Out of scope:** migrasi backlog, otomasi backup, perubahan kode selain upgrade Beads.
- **Known edge case:** `make install` me-restart daemon yang sedang jalan; gunakan `make build` + salin manual, atau `make safe-install`, saat town aktif.
- **Known limitation:** `wisp_reaper` mode `dry_run` juga menonaktifkan pembersihan wisp; ukuran DB town bisa tumbuh selama pilot (dipantau, bukan dimitigasi).

## 4. Perlu tindakan Anda

- Persetujuan PR upgrade Beads ke `main` fork.
- Konfirmasi lokasi `$TOWN` dan `$BK`.
- Menjalankan epic manual untuk baseline (Langkah 7, Epic A).
