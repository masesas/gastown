# Baseline Test Fork (P-S0 Langkah 1)

Tanggal: 12 Oktober 2026. Commit: `381ce8fb` (branch `docs/enh-masesas-reanalisis`; kode Go identik dengan `main` `649b832b`). Bukti S0-AC2.

## Environment

| Komponen | Versi |
| --- | --- |
| Go | 1.27.1 darwin/arm64 |
| `bd` CLI | 1.3.0 |
| Beads embed (`go.mod`) | v1.0.5 |
| Dolt | 1.83.3 (test memakai container `dolthub/dolt-sql-server:2.0.7` via testcontainers) |
| Docker | Desktop 29.1.3 |
| tmux | **tidak terpasang** |
| Build | `make build` sukses; `gt version 381ce8fb-dirty` |

Perintah: `go test ./...` dengan flag ICU (`CGO_CPPFLAGS/CGO_LDFLAGS` dari `brew --prefix icu4c`). Durasi ±8,5 menit.

## Hasil

67 paket `ok`, **7 paket FAIL, 27 test gagal**. Catatan sebelumnya ("2 test `internal/beads` gagal") tidak lengkap; angka yang benar adalah 27.

Setiap test gagal dijalankan ulang terisolasi untuk memisahkan penyebab.

| Kelompok | Penyebab | Test | Bukti | Klasifikasi |
| --- | --- | --- | --- | --- |
| A (7) | `TMPDIR` macOS ada di bawah symlink `/var` → `/private/var`. Kode membandingkan path yang sebagian di-resolve (`EvalSymlinks`) dengan path yang tidak | beads: `TestGetRigDirForName`, `TestRunEnv_StripsPollutedDoltEnvAndUsesRigMetadata`; cmd: `TestResolveAgentTrackingBeadsDirPrefersCwdRigRedirectOverBeadsDir`, `TestRunAgentStateUsesCwdRigBeadsDirWhenBeadsDirPointsTown`; daemon: `TestHasAssignedOpenWork_UsesPinnedBeadsDirInsteadOfRigOrRepoFlag`; git: `TestNestedWorkDirResolvingToTownRootGitIsBlocked`; polecat: `TestAddWithOptions_NoFilesAddedToRepo` | Semua lulus dengan `TMPDIR` non-symlink | **Environment**, dengan satu bug laten (lihat bawah) |
| B (7) | `tmux` tidak terpasang | cmd: `TestExecuteWarrants_MarksPendingAsExecuted`; daemon: `TestReapIdleDogs_RemovesLongIdleDogsWhenPoolOversized`, `TestReapIdleDogs_StopsRemovingAtMaxPoolSize`, `TestReapIdleDogs_MixedStates`, `TestFindDispatchableDog_PicksFirstIdleWhenNoSessionsLive`; mail: `TestNotifyRecipient_CanonicalAliasQueuesAllHeadlessCandidates`, `TestNotifyRecipient_DogQueuesDogSessionNotDeacon` | Pesan `exec: "tmux": executable file not found in $PATH` | **Environment**; juga prasyarat runtime Gas Town |
| C (12) | `bd` 1.3.0 menolak membuka DB: `PROJECT IDENTITY MISMATCH — refusing to connect` (project ID `metadata.json` per test ≠ project ID DB di server Dolt test yang dipakai bersama) | refinery: 12 test `TestEngineerCloseMRWithReason*` dan `TestManager_*` (PostMerge, RejectMR, Queue, TerminalClose) | Konsisten 3/3 run paket; test tunggal kadang lulus (DB belum dimiliki test lain) | **Version skew** (`bd` CLI 1.3.0 vs perilaku yang diasumsikan test). [I] Cek identitas proyek kemungkinan fitur `bd` yang lebih baru dari 1.0.5; diverifikasi di spike upgrade Beads |
| D (1) | Gagal di full suite, lulus terisolasi | polecat: `TestAddWithOptions_SettingsInstalledInPolecatsDir` | Lulus saat dijalankan sendiri | **Flaky** di bawah beban paralel; pantau setelah upgrade |

### Bug laten dari kelompok A

`pathWithin` (`internal/beads/routes.go:437-451`) me-resolve symlink pada `root`, tetapi `EvalSymlinks(path)` gagal bila path belum ada sehingga path tetap tanpa resolve. Akibatnya `GetRigDirForName` mengembalikan `""` untuk rig yang direktorinya belum ada ketika town root berada di bawah symlink. Dampak di mesin ini kecil selama `$TOWN` tidak berada di bawah path symlink (mis. `~/gt-pilot` aman). Tidak diperbaiki sekarang; dicatat sebagai bead perbaikan opsional.

Kegagalan `TestRunEnv_*` adalah asumsi test (stub membandingkan `BEADS_DIR` yang tidak di-resolve dengan `$PWD` yang sudah di-resolve), bukan bug kode.

## Dampak ke rencana

| Temuan | Tindakan |
| --- | --- |
| tmux tidak terpasang | **Prasyarat baru S0**: `brew install tmux` sebelum pilot. Gas Town menjalankan semua agent di sesi tmux |
| Kelompok C menunjukkan skew `bd` 1.3.0 vs embed 1.0.5 sudah berdampak di test | Memperkuat D2-A. Kriteria lulus spike upgrade Beads: 12 test refinery hijau |
| Baseline test untuk membandingkan regresi | Gunakan `TMPDIR` non-symlink dan tmux terpasang. Setelah keduanya, baseline yang diharapkan: hanya kelompok C (+ D bila flaky) yang tersisa |

Log lengkap: `go-test-all.txt` di scratchpad sesi (tidak di-commit).
