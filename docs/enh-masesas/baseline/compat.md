# Catatan Kompatibilitas Pilot S0

Tanggal: 14 Oktober 2026. Bukti S0-AC1 dan S0-AC10. Diperbarui setiap akhir tahap (aturan lintas tahap `../11` § 1).

## Versi

| Komponen | Versi | Sumber |
| --- | --- | --- |
| Fork Gas Town (`gt` di pilot) | `7eb40030` (`main`, PR #17 dan #18) | `gt version` di pilot: `gt version 7eb40030-dirty (dev: main@7eb40030)`. "dirty" = file `.beads` lokal fork, bukan kode |
| Beads embed | `github.com/steveyegge/beads v1.3.0` | `go list -m github.com/steveyegge/beads` |
| `bd` CLI | 1.3.0 (`f45b249ce`) | `bd version`. Sama dengan embed, sehingga fallback C (bd terisolasi) tidak dipakai |
| Dolt (pilot) | 2.3.5, binary terisolasi di `$TOWN/../tools/bin/dolt` | `dolt version`. Dolt sistem (1.83.3) tetap dipakai project lain dan tidak diubah |
| Claude Code | 2.1.299 (pilot diuji mulai 2.1.296) | `claude --version`. Diperbarui otomatis secara global; dialog trust 2.1.296 diatasi PR #12 dan #13 |
| Antigravity CLI (`agy`) | 1.3.5 (spike dimulai 1.3.4) | Binary di `$TOWN/../tools/agy/agy`, TeamID `EQHXZ8M8AV`. Auto-updater tetap mengganti binary walau `AGY_CLI_DISABLE_AUTO_UPDATE=1`; versi tidak terkunci |
| tmux | 3.8 | `tmux -V` |
| git | 2.52.0 | `git --version` |
| Go | 1.27.1 darwin/arm64 | `go version` |
| macOS | 27.0 | `sw_vers` |
| Node / pnpm (rig mobile) | v24.12.0 / 10.27.0 | gate `posin-gate` mobile |

## Konfigurasi pilot yang memengaruhi kompatibilitas

| Hal | Nilai |
| --- | --- |
| Port Dolt town | 3417 (bukan default; server Dolt project lain tetap di port masing-masing) |
| Config Claude agent | `CLAUDE_CONFIG_DIR=$TOWN/../claude-config` (salinan terkurasi, bukan `~/.claude`) |
| Cost tier | `economy`: mayor/witness/refinery `claude-sonnet`, deacon/boot/dog `claude-haiku`, polecat/crew default (Opus) |
| Patrol mati | `dolt_backup`, `jsonl_git_backup`, `compactor_dog`, `scheduled_maintenance`, `doctor_dog`, `main_branch_test`; `wisp_reaper.dry_run=true`; `quota_dog` tidak dikonfigurasi (default mati, `IsPatrolEnabled` di `internal/daemon/types.go`) |

## Ketidakcocokan yang diketahui

| Versi | Masalah | Status |
| --- | --- | --- |
| Claude Code 2.1.296 | Dialog trust folder tidak terbaca oleh deteksi lama; agent macet di dialog | Diperbaiki PR #12 (gt-swqb) dan #13 (gt-i81z, fail-closed) |
| git 2.52 + worktree | `info/exclude` per-worktree diabaikan git; `gt done` polecat terblokir | Diperbaiki PR #14 (gt-zjlv) |
| Claude Code 2.1.296 | Nudge mengirim Escape saat tool Bash berjalan; gate refinery terputus | Diperbaiki PR #16 (gt-dzck) |
| bd 1.3.0 circuit breaker | `gt up` cold start: cek safety stop refinery gagal fail-fast | Diperbaiki PR #17 (gt-pd1c.38) |
| `agy` 1.3.x | Prompt hanya lewat `-i`/`-p`/stdin; gt menyisipkan prompt berdasarkan nama perintah | Wrapper `agy-gt` di pilot; perbaikan kode gt-pd1c.34 |
