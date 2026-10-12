# SP0 — Spike Upgrade Beads v1.0.5 → v1.3.0

Tanggal: 12–13 Oktober 2026. Plan: P-S0 Langkah 2 (D2-A). Bead: `gt-pd1c.1`. Branch: `chore/beads-upgrade` (commit `a5ac1e6a`, worktree `../gastown-beads-upgrade`). Durasi: < 1 hari dari timebox 2 hari.

## Kesimpulan

**Lulus.** Upgrade ke `github.com/steveyegge/beads v1.3.0` tidak butuh perubahan kode produksi, tidak menambah kegagalan test, dan wajib untuk pilot: dengan v1.0.5 daemon menolak store yang ditulis `bd` 1.3.0.

## Perubahan

| File | Isi |
| --- | --- |
| `go.mod`/`go.sum` | beads v1.3.0; dependensi transitif ikut naik (Dolt, go-mysql-server, `dolthub/driver` → `/v2`, testcontainers 0.43, `golang.org/x/*`); direktif `go` 1.26.2 → 1.26.5 |
| `internal/testutil/doltserver.go` | Container Dolt test ditandai `BEADS_TEST_SERVER=1`. Beads ≥ 1.3 menolak membuka database `testdb_*` di server tanpa tanda ini, sehingga **semua test store SDK ter-skip diam-diam** |
| `internal/daemon/daemon_compatibility_test.go` | Test regresi: embed beads menerima store `bd_version=1.3.0`. RED di `main` ("embedded beads 1.0.5 rejects a store written by bd 1.3.0"), GREEN di spike |

## Bukti

| Cek | Hasil |
| --- | --- |
| `go build`, `go vet`, `golangci-lint` (paket berubah) | Bersih |
| Suite penuh (TMPDIR non-symlink, tmux, `-ldflags BuiltProperly=1`, timeout 30m) | 11.751 pass, 22 skip, 24 fail. **0 kegagalan baru** dibanding `main`; 223 test tambahan berjalan karena paket `daemon` tidak lagi timeout |
| Kegagalan tersisa (sama dengan `main`) | refinery 12 (`gt-pd1c.3`), `prime` external tools 4 (flaky di bawah beban, lulus terisolasi), polecat 2 (flaky) |
| Test kompatibilitas daemon terisolasi | 3/3 pass, tanpa env manual |

## Performa

Suite paket `convoy`: 310 s (spike) vs 95 s (`main`), hasil sama (72 pass, 1 skip). Pengukuran terpisah:

| | Buka store | Create/op | `isIssueBlocked`/op |
| --- | --- | --- | --- |
| v1.0.5 | 0,8–1,5 s | 17–23 ms | 2–7 ms |
| v1.3.0 | 1,6–1,8 s | 12–16 ms | 0,6–1 ms |

Operasi runtime tidak lebih lambat. Perlambatan suite berasal dari pembuatan database per test di container bersama yang menumpuk. Dampak ke daemon: hanya waktu start (buka store sekali). **Tidak memblokir pilot.**

## Temuan sampingan (bead terpisah)

| Bead | Temuan |
| --- | --- |
| `gt-pd1c.2` | `go test ./...` meninggalkan proses `dolt sql-server` yatim (±75 dari tiga run; 13 dari satu run spike). Dibersihkan manual oleh user setelah verifikasi PID |
| `gt-pd1c.3` | 12 test refinery: `bd init --prefix gt --server` ke container bersama → database sama → `PROJECT IDENTITY MISMATCH` di `bd` 1.3.0. Bukan regresi upgrade |
| `gt-pd1c.4` | Paket `cmd` berhenti di tengah di macOS (`os.Exit` dari cek `BuiltProperly`) tanpa `-ldflags` |

## Pelajaran operasional

- Jangan menyetel `BEADS_TEST_SERVER=1` di level shell: env itu mematikan dua pengaman beads terhadap server non-test untuk seluruh run. Cukup di helper test yang terikat ke container khusus.
- `bd init` di subfolder repo menulis ke `.beads` repo (naik ke root git), memasang `core.hooksPath` dan integrasi Codex/Cursor/Claude. Semua sudah dikembalikan; tracker kerja memakai database lokal `.beads/embeddeddolt` (ignored) dengan label `enh-masesas`. **Jangan pernah `bd sync`/`bd dolt push`**: `sync.remote` repo mengarah ke upstream.

## Langkah berikutnya

PR `chore/beads-upgrade` → `main` fork (butuh approval Anda). Setelah merge: P-S0 Langkah 3–8 (`gt-pd1c.17`).
