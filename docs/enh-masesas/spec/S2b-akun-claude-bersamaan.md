# S2b — Akun Claude Bersamaan dengan Satu Config Global

Asal: Rank 4a (`../09` § 4), `../13` (invariant I1–I7, opsi A), keputusan D10, Q12. Tahap 2. **Bergantung pada spike SP1** (`S2c`). Harus di-merge setelah S1b.

## 1. Masalah

- Saat satu akun Claude terkena limit, polecat dianggap stalled dan di-restart berulang ke akun yang sama.
- Mekanisme akun yang ada memecah config: `gt account add` membuat config dir hampir kosong (skills, rules, hooks, plugins, MCP, history hilang), `account switch` bisa menghapus direktori, dan rotasi keychain menulis entri Keychain default yang dikelola claude-swap.

## 2. Invariant (wajib)

| ID | Invariant |
| --- | --- |
| I1 | Satu config global: semua sesi agent memakai `~/.claude` dan `~/.claude.json` yang sama. Gas Town tidak membuat config dir per akun |
| I2 | Yang berbeda per akun hanya kredensial |
| I4 | Gas Town tidak menulis entri Keychain default `Claude Code-credentials` maupun `oauthAccount` |
| I5 | Token tidak muncul di argumen command line, string respawn tmux, environment tmux global, `accounts.json`, atau log |
| I6 | Satu kredensial OAuth tidak dipakai di dua home |
| I7 | Failover tidak memutus `--resume` dan history |

## 3. Perilaku yang diharapkan

- **Registrasi akun**: akun Gas Town menyimpan referensi ke item Keychain milik Gas Town yang berisi token `claude setup-token`. Token dimasukkan lewat stdin/prompt, tidak lewat argumen. Tidak ada config dir yang dibuat.
- **Peluncuran sesi**: launcher mengambil token saat exec, men-set `CLAUDE_CODE_OAUTH_TOKEN`, dan meng-unset `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `CLAUDE_CONFIG_DIR` untuk proses itu saja.
- **Pemilihan akun**: spawn tanpa akun eksplisit memilih akun non-limited dengan pemakaian terendah. Data pemakaian dari deteksi pane; `cswap list --json` dipakai bila tersedia (opsional).
- **Deteksi limit**: witness/daemon mengklasifikasikan pesan usage limit di pane polecat sebagai status `limited` (bukan stalled), mencatat waktu reset, dan menyimpan status itu secara persisten.
- **Failover**: sesi `limited` di-restart dengan akun non-limited berikutnya + `--resume <session-id>`. Bila semua akun limited: tidak ada restart, status `limited` + eskalasi ke Mayor, dan sesi dilanjutkan otomatis setelah waktu reset terdekat.
- **Dinonaktifkan untuk Claude**: rotasi keychain bawaan, `gt account switch`, pembuatan config dir oleh `gt account add`. `gt doctor` memperingatkan bila `CLAUDE_CONFIG_DIR` di-set pada sesi agent dan bila token mendekati kedaluwarsa (≤ 30 hari).
- **Event**: `session.lifecycle{state: limited|rotated, account, provider, reason}`.

## 4. Acceptance criteria

- [ ] **S2b-AC1** Pane polecat berisi pesan usage limit → status `limited`; tidak ada restart ke akun yang sama.
- [ ] **S2b-AC2** Rotasi memilih akun non-limited lain, menyimpan status limited akun asal, dan tidak memanggil penulisan Keychain default maupun `oauthAccount` (I4).
- [ ] **S2b-AC3** Dua polecat berjalan bersamaan dengan akun berbeda, memakai `~/.claude` + `~/.claude.json` yang sama; skill, rule, hook, dan MCP yang sama tersedia di kedua sesi (I1).
- [ ] **S2b-AC4** Tidak ada direktori config baru yang dibuat Gas Town; isi `~/.claude` tidak berubah oleh operasi akun (I1, I2).
- [ ] **S2b-AC5** `cswap switch` saat polecat berjalan tidak mengubah akun polecat.
- [ ] **S2b-AC6** Rotasi melanjutkan transkrip yang sama (`--resume`) dengan akun lain (I7).
- [ ] **S2b-AC7** Token tidak muncul di `ps`, environment tmux, `accounts.json`, maupun log (I5).
- [ ] **S2b-AC8** Semua akun limited → tidak ada loop restart; eskalasi tercatat satu kali; lanjut setelah reset.
- [ ] **S2b-AC9** `gt account switch` dan rotasi keychain menolak dijalankan untuk akun Claude mode token, dengan pesan yang menjelaskan alasannya.
- [ ] **S2b-AC10** Event `session.lifecycle` memuat akun dan provider.

## 5. Di luar scope

Fallback provider lain (Rank 4b, spec terpisah setelah SP2/SP3); kapasitas per akun di scheduler (E02); perubahan claude-swap; Linux; claude.ai connectors dan Remote Control (Q12: tidak dibutuhkan).

## 6. Rollout dan rollback

`quota_dog` dinyalakan di pilot setelah AC1–AC9 lulus; pantau event `limited`. Rollback: `quota_dog` off; sesi kembali ke login Keychain default.

## 7. Gate keluar (bersama S2a)

Dua polecat berjalan bersamaan dengan akun berbeda di atas satu `~/.claude`; polecat yang terkena limit dilanjutkan dengan akun lain (`--resume`) tanpa restart berulang dan tanpa menyentuh login claude-swap.

## Riwayat

- 12 Okt 2026: dibuat dari Rank 4a dan `13`. Ditambahkan: token lewat stdin (I5), lanjut otomatis setelah reset (AC8), penolakan eksplisit `account switch` (AC9), ambang peringatan kedaluwarsa 30 hari.
