# S2c — Spike E01 (Kredensial per Akun)

Asal: `../13` § 8, Rank 4b(a). Awal Tahap 2. Hasil spike menentukan apakah S2b berjalan apa adanya dan bagaimana spec Rank 4b ditulis.

## 1. Aturan umum

- Spike dijalankan di direktori sementara dan akun uji. `~/.claude`, `~/.codex`, `~/.gemini`, dan entri Keychain default tidak diubah.
- Token dan isi file kredensial tidak dicetak ke terminal, log, atau dokumen. Laporan hanya memuat hasil perilaku dan nama file.
- Timebox per spike: 1–2 hari. Hasil ditulis ke `../spike/SPn-*.md` dengan format: pertanyaan, langkah, hasil, kesimpulan, dampak ke spec.

## 2. Daftar spike

| ID | Pertanyaan | Lulus bila | Dampak bila gagal |
| --- | --- | --- | --- |
| SP1a | Apakah `CLAUDE_CODE_OAUTH_TOKEN` (akun B) dipakai walau Keychain default berisi login akun A? | Usage tercatat di akun B; `cswap switch` di tengah sesi tidak memindahkan sesi | S2b beralih ke opsi C (home per akun + denylist), effort naik ke M–L |
| SP1b | Apakah `--resume <id>` untuk sesi yang dibuat dengan akun A berjalan dengan token akun B? | Transkrip dilanjutkan tanpa error | Failover memakai sesi baru + konteks bead (I7 dilonggarkan, perlu keputusan Anda) |
| SP1c | Apakah pesan usage limit di pane sama untuk sesi `setup-token`? | Pola deteksi yang ada cocok, atau pola baru tercatat | Tambah pola deteksi di S2b |
| SP1d | Apakah token bisa diambil launcher dari item Keychain milik Gas Town tanpa muncul di `ps`? | Mekanisme terbukti (mis. API Keychain langsung, bukan argumen `security`) | Desain launcher diubah sebelum impl plan S2b |
| SP2 | Codex: dengan `CODEX_HOME` per akun dan symlink ke `~/.codex`, di mana SQLite menaruh `-wal`/`-shm`? Apakah keyring terikat ke `CODEX_HOME`? | Dua akun bersamaan tidak merusak DB; kredensial terpisah | Codex fallback memakai API key (`codex exec`) atau ditunda |
| SP3 | Gemini: di mana kredensial OAuth disimpan pada versi terpasang; apakah `GEMINI_CLI_HOME` per akun cukup? | Dua akun bersamaan dengan config bersama | Gemini fallback memakai API key atau ditunda |

## 3. Acceptance criteria

- [ ] **S2c-AC1** Laporan SP1a–SP1d selesai sebelum impl plan S2b ditulis.
- [ ] **S2c-AC2** Laporan SP2 dan SP3 selesai sebelum spec Rank 4b ditulis.
- [ ] **S2c-AC3** Tidak ada perubahan pada config global dan Keychain default (diverifikasi dengan perbandingan daftar nama file dan mtime sebelum/sesudah).
- [ ] **S2c-AC4** Setiap kegagalan spike disertai keputusan yang Anda ambil atau pertanyaan terbuka.

## Riwayat

- 12 Okt 2026: dibuat dari `13` § 8. SP1d ditambahkan karena token saat ini dilewatkan di argumen `security` (`13` K24, I5).
