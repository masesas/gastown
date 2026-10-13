# Re-analisis Revisi 2 — E01 dengan Satu Config Global per Runtime

Tanggal: 12 Oktober 2026. Legenda bukti: [V] terverifikasi di source/mesin ini, [D] dokumentasi resmi (dibaca, belum diuji), [U] laporan upstream/issue, [I] inferensi, [A] asumsi.

## 1. Input dan kebutuhan

Anda sudah memakai **claude-swap** untuk berpindah akun Claude. Kebutuhan baru:

> Beberapa akun dipakai **bersamaan**, tetapi semuanya memakai **satu** folder config global yang sama: config, history, skills, hooks, commands, rules, dan file bernilai lainnya. Implementasi E01 tidak boleh membuat folder config baru yang memecah atau merusak isi tersebut. Ini berlaku juga untuk agent lain (Codex, Gemini).

Kebutuhan ini saya tulis sebagai invariant E01 (§ 4). Invariant ini **mengoreksi rekomendasi Revisi 1** (§ 2).

## 2. Koreksi atas Revisi 1

| No | Klaim Revisi 1 | Status | Dasar |
| --- | --- | --- | --- |
| K21 | D10 / Rank 4a: rotasi memakai Rotator swap `CLAUDE_CONFIG_DIR`, "setiap akun tetap di direktorinya sendiri" | **Dibatalkan** | Pola ini justru membuat config dir per akun. Hasilnya: skills, rules, plugins, settings, dan history terpecah atau hilang. Ini bertentangan dengan kebutuhan Anda. Rotator juga hanya me-link transkrip, bukan history/settings [V] `quota/executor.go:189-244`, `cmd/seance.go:601-717` |
| K22 | `12` § 3.1: `gt account add` membuat direktori config "per akun" | **Dipertajam** | [V] Direktori yang dibuat **hampir kosong**: hanya `commands/` yang di-symlink ke `~/.claude/commands` (`cmd/account.go:188-197`, `:470-504`). Sesi pada akun itu kehilangan skills, agents, rules, plugins, settings (termasuk hooks user), `CLAUDE.md`, history, dan MCP server dari `~/.claude.json` |
| K23 | Risiko `account switch` | **Diperberat** | [V] Bila `~/.claude` adalah direktori nyata, `switch` menjalankan `os.RemoveAll(<config dir akun default>)` lalu memindahkan `~/.claude` ke sana tanpa konfirmasi (`cmd/account.go:413-436`) |
| K24 | Rotasi keychain bawaan hanya "menyalin token" | **Ditambah** | [V] Rotasi menulis entri keychain default `Claude Code-credentials` saat sesi memakai `~/.claude` (`quota/keychain.go:35-51`), yaitu entri yang sama yang dikelola claude-swap. Swap `oauthAccount` mencari `<configDir>/.claude.json`. Untuk config default, file itu ada di `~/.claude.json`, bukan `~/.claude/.claude.json`, sehingga swap identitas hampir pasti tidak terjadi (`keychain.go:125-178`) [I] |

## 3. Fakta yang menentukan desain

### 3.1 Claude Code

| Fakta | Bukti |
| --- | --- |
| `CLAUDE_CONFIG_DIR` memindahkan **seluruh** isi `~/.claude`: settings (termasuk hooks), `CLAUDE.md`, skills, commands, agents, rules, plugins, `projects/` (transkrip + auto memory), `history.jsonl`, file-history, dan lainnya | [D] code.claude.com/docs/en/env-vars, `/claude-directory` |
| `~/.claude.json` menyimpan sesi login (`oauthAccount`), **MCP server user-scope**, state per-project (trust), dan key global. Karena itu file ini tidak bisa begitu saja dibagi antar-akun bila tiap akun punya config dir sendiri | [D] `/settings` |
| Kredensial macOS ada di Keychain. Entri dikunci per config dir (`Claude Code-credentials-<hash>`). `CLAUDE_CONFIG_DIR=~/.claude` yang di-set eksplisit memilih entri berbeda dari default | [D] `/authentication`; [V] claude-swap `session.py:232-243`; [U] anthropics/claude-code#92252 |
| `CLAUDE_CODE_OAUTH_TOKEN` (dibuat `claude setup-token`, berlaku 1 tahun) **mengalahkan** kredensial Keychain dan bekerja per proses. Batasan: tanpa claude.ai connectors dan Remote Control (MCP lokal tetap jalan); token kedaluwarsa butuh restart. Urutan prioritas: `ANTHROPIC_AUTH_TOKEN` dan `ANTHROPIC_API_KEY` berada di atasnya | [D] `/authentication#authentication-precedence` |
| `--resume`/`--continue` membaca transkrip dari `<config dir>/projects/` | [D] `/claude-directory` |
| Di mesin ini: `~/.claude` adalah direktori nyata berisi settings.json, AGENTS.md, agents, commands, skills, rules, hooks, plugins, projects, history.jsonl, dan direktori lain (ecc, homunculus, metrics, …). Tidak ada `.credentials.json` (Keychain dipakai). `CLAUDE_CONFIG_DIR` tidak di-set | [V] listing nama saja |

### 3.2 claude-swap v0.26.0 (terpasang di mesin ini)

| Mode | Cara kerja | Apakah memenuhi kebutuhan Anda? |
| --- | --- | --- |
| `cswap switch` / `auto` (global) | Mengganti entri Keychain `Claude Code-credentials` dan hanya key `oauthAccount` di `~/.claude.json`. Tidak menyentuh `~/.claude/`. Sesi yang sedang jalan memakai akun baru di pesan berikutnya (Keychain di-cache ±30 detik) [V] `switcher.py:7008-7032`, `:7084-7105` | Satu config: **ya**. Beberapa akun **bersamaan**: **tidak**, karena semua sesi default ikut pindah |
| `cswap run N` (per sesi) | Profil persisten `~/.claude-swap-backup/sessions/<n>-<email>/` sebagai `CLAUDE_CONFIG_DIR`. Symlink: `settings.json`, `keybindings.json`, `CLAUDE.md`, `skills`, `commands`, `agents`. `projects/` + `history.jsonl` hanya dengan `--share-history`. **Tidak dibagi:** `plugins/`, `rules/`, direktori lain. `mcpServers` disalin dari `~/.claude.json` (bukan live) [V] `session.py:76-90`, `:876-890`, `:1018-1130` | Bersamaan: **ya**. Satu config: **sebagian**, karena rules, plugins, dan direktori kustom tidak ikut, MCP bisa diverge, dan history opsional |
| Pemantauan kuota | Polling `api.anthropic.com/api/oauth/usage` per akun; `cswap list --json` (schemaVersion 1) memuat usage per akun [V] `oauth.py:366`, `json_output.py:211-251` | Berguna sebagai **sumber data kuota** untuk Gas Town |

Catatan pitfall dari claude-swap dan dokumen: Claude Code menulis ulang `settings.json` dan `.claude.json` lewat rename. Akibatnya **symlink file** bisa berubah menjadi file biasa, lalu diverge diam-diam. Symlink **direktori** tidak terkena masalah ini [D][I].

### 3.3 Codex dan Gemini

| Runtime | Relokasi config | Kredensial per proses | Isi lokal (nama saja) |
| --- | --- | --- | --- |
| Codex | `CODEX_HOME` (default `~/.codex`) [D] | `CODEX_API_KEY` hanya untuk `codex exec` dan berbasis billing API. Login ChatGPT tersimpan di `auth.json` (`cli_auth_credentials_store=file`) atau keyring [D] | config.toml, auth.json, AGENTS.md, hooks.json, rules, skills, plugins, memories, sessions, history.jsonl, plus beberapa DB SQLite (+ `-wal`/`-shm`) [V] |
| Gemini | `GEMINI_CLI_HOME` (CLI memakai `$GEMINI_CLI_HOME/.gemini/`) [D] | `GEMINI_API_KEY`, `GOOGLE_API_KEY`, Vertex/ADC [D]. OAuth di `oauth_creds.json`; build baru mungkin di Keychain [U, belum diverifikasi] | settings.json, GEMINI.md, oauth_creds.json, google_accounts.json, history, tmp, … [V] |
| Gas Town hari ini | Codex/Gemini tanpa `ConfigDirEnv`; `AgentEnv` selalu memakai nama `CLAUDE_CONFIG_DIR` [V] `config/agents.go:257-300`, `config/env.go:178` | — | — |

Gas Town menulis hooks dan slash command **di dalam tree town** (`<role-dir>/.claude/settings.json` lewat `--settings`; `<workDir>/.claude/commands/`), bukan di config dir user [V] `hooks/installer.go:48-72`, `hooks/config.go:559-629`, `templates/commands/provision.go:41-130`. Pengecualian: `gt seance` menulis ke `~/.claude/projects` [V] `cmd/seance.go:545-772`. Artinya, config user hanya perlu dijaga dari mekanisme akun, bukan dari hooks Gas Town.

## 4. Invariant E01 (wajib untuk semua runtime)

| ID | Invariant | Cara verifikasi |
| --- | --- | --- |
| I1 | **Satu config global per runtime**: `~/.claude`, `~/.codex`, `~/.gemini`. Gas Town tidak membuat salinan config yang bisa diverge | `gt doctor`: tidak ada config dir akun yang berisi file/direktori config nyata selain kredensial |
| I2 | **Yang berbeda per akun hanya kredensial**, ditambah state login minimal yang secara teknis tidak bisa dipisah | Inventaris per runtime (§ 5) |
| I3 | Bila home per akun tidak bisa dihindari: bagikan dengan **denylist** ("semua kecuali kredensial dan state login"), bukan allowlist, supaya item baru (rules, plugins, direktori kustom) otomatis ikut. Utamakan symlink direktori. Symlink file yang ditulis ulang via rename dicek dan dipulihkan | `gt doctor`: setiap entri di home akun resolve ke target global yang sama |
| I4 | Gas Town **tidak menyentuh** entri Keychain default dan `oauthAccount` di `~/.claude.json`. Wilayah itu milik claude-swap/login interaktif Anda | Test: rotasi Gas Town tidak memanggil `security add-generic-password` untuk service default |
| I5 | Token tidak pernah muncul di argumen command line, string respawn tmux, `accounts.json`, atau log | Test + grep log; token diambil launcher dari Keychain saat `exec` |
| I6 | Satu login OAuth (refresh token yang sama) **tidak pernah** dipakai di dua home. Refresh token berotasi, sehingga salinan lain akan logout | Registry akun memetakan satu kredensial ke satu pemakaian |
| I7 | Failover tidak boleh memutus `--resume` atau history | Test: rotasi akun → sesi baru melanjutkan transkrip yang sama |

## 5. Opsi desain untuk Claude

| Opsi | Mekanisme | Satu config? | Bersamaan? | Kelemahan |
| --- | --- | --- | --- | --- |
| **A. Token per proses** | Satu `setup-token` per akun disimpan di item Keychain milik Gas Town. Launcher mengambilnya saat `exec`, men-set `CLAUDE_CODE_OAUTH_TOKEN`, meng-unset `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN`/`CLAUDE_CONFIG_DIR`, lalu menjalankan `claude` | **Penuh**: `~/.claude` dan `~/.claude.json` sama untuk semua akun | Ya | Tanpa claude.ai connectors dan Remote Control; token 1 tahun dirotasi manual; ganti akun = restart sesi; `/status` tetap menampilkan login Keychain [D] |
| B. Pakai `cswap run` | Gas Town meluncurkan `cswap run N --require-session -- claude …` | Sebagian (rules, plugins, direktori kustom tidak ikut; MCP disalin; history opsional) | Ya | Bergantung pada daftar symlink claude-swap; perlu patch upstream atau langkah tambahan; `projects/` harus `--share-history` agar resume lintas akun jalan |
| C. Home per akun buatan Gas Town (denylist symlink) | `CLAUDE_CONFIG_DIR` per akun; symlink semua entri `~/.claude` kecuali kredensial/state login; `.claude.json` per akun dengan sinkronisasi `mcpServers` | Hampir penuh | Ya | `.claude.json` tetap terpecah (trust per project, MCP perlu sinkron); symlink file bisa putus; paling banyak kode dan maintenance |
| D. Swap global (claude-swap `switch` / rotasi keychain Gas Town) | Satu akun aktif untuk semua sesi | Ya | **Tidak** | Tidak memenuhi kebutuhan "bersamaan" |

**Rekomendasi: A untuk sesi agent Gas Town (polecat, crew, refinery, witness), dan claude-swap tetap untuk sesi interaktif Anda.**

Alasan:

- Hanya A yang memenuhi I1 sepenuhnya, termasuk `~/.claude.json` (MCP, trust project) dan `projects/`. Akibatnya failover cukup berupa restart dengan token lain + `--resume <session-id>`, dan transkrip tetap ditemukan (I7).
- A tidak menyentuh Keychain default maupun `oauthAccount`. Jadi tidak bertabrakan dengan claude-swap, termasuk saat `cswap auto` berpindah akun untuk sesi interaktif Anda. Token per proses mengalahkan Keychain [D].
- `setup-token` adalah grant terpisah dari login yang disimpan claude-swap, sehingga I6 terpenuhi [I].
- Batasan A (tanpa connectors claude.ai dan Remote Control) umumnya tidak relevan untuk worker otomatis. Perlu dicek apakah ada MCP yang Anda pakai lewat connector claude.ai (Q12).

Fallback bila A ditolak karena batasan tersebut: **C**, dengan denylist dan pemeriksaan `gt doctor`. B hanya layak bila daftar share claude-swap diperluas (rules, plugins, semua direktori) di upstream.

**Peran claude-swap dalam desain A:** sumber data kuota (`cswap list --json`, usage 5 jam/7 hari per akun), dipakai Gas Town untuk memilih akun sebelum limit tercapai. Integrasi ini opsional. Tanpa claude-swap, Gas Town tetap memakai deteksi limit dari pane (sudah ada, `quota/scan.go`). Pemetaan akun Gas Town ↔ slot claude-swap memakai email.

## 6. Desain untuk Codex dan Gemini (4b)

Prinsip sama, tetapi kedua CLI tidak punya token per proses yang setara untuk login berlangganan. Akibatnya home per akun (opsi C) kemungkinan **tidak bisa dihindari** untuk login OAuth.

- **Codex**
  - Login ChatGPT: `CODEX_HOME` per akun yang berisi `auth.json` milik akun itu. Semua entri lain di-symlink ke `~/.codex` dengan denylist.
  - Bagian berisiko: **DB SQLite** (`state_5`, `memories_1`, dll.). Belum diverifikasi apakah Codex/SQLite menaruh file `-wal`/`-shm` di samping target symlink atau di samping path symlink. Jika di samping path symlink, dua akun bisa merusak DB yang sama. Ini wajib di-spike sebelum dipakai [A].
  - Alternatif tanpa home per akun: `CODEX_API_KEY` untuk `codex exec` (billing API, bukan langganan).
- **Gemini**
  - Login OAuth: `GEMINI_CLI_HOME` per akun dengan `.gemini/oauth_creds.json` + `google_accounts.json` sendiri, dan sisanya di-symlink. Perlu dicek apakah build terbaru memakai Keychain [U].
  - Alternatif: `GEMINI_API_KEY` per proses dengan `~/.gemini` bersama, sehingga I1 terpenuhi penuh tetapi dengan billing API.

Karena itu 4b dimulai dengan **spike per provider** (§ 7) sebelum kode.

## 7. Scope Rank 4 yang direvisi

**4a — Akun Claude bersamaan dengan satu config global** (effort **M**, keyakinan sedang)

- (a) Registry akun Gas Town menyimpan **referensi** item Keychain per handle (bukan token, bukan config dir). Mode `claude_auth: token-env`.
- (b) Launcher `gt` mengambil token dari Keychain saat `exec`, men-set `CLAUDE_CODE_OAUTH_TOKEN`, meng-unset variabel auth lain dan `CLAUDE_CONFIG_DIR`. Token tidak masuk string respawn tmux (I5).
- (c) Klasifikasi usage-limit untuk polecat/witness → status `limited` (tetap dari Revisi 1).
- (d) Failover: restart sesi dengan akun non-limited berikutnya + `--resume <session-id>`.
- (e) Status limited disimpan dengan waktu reset (dari pane, atau dari `cswap list --json` bila tersedia).
- (f) Spawn tanpa akun eksplisit memilih akun non-limited dengan usage terendah.
- (g) **Matikan untuk Claude:** jalur rotasi keychain (`executeKeychainRotation`), `gt account switch`, dan pembuatan config dir oleh `gt account add`. `gt doctor` memperingatkan bila `CLAUDE_CONFIG_DIR` di-set pada sesi agent.
- (h) Test I4–I7 dan alur "semua akun limited → eskalasi, tanpa loop restart".

**4b — Fallback provider** (effort **M–L**, keyakinan rendah)

- Spike 1–2 hari per provider: Codex (`CODEX_HOME` + denylist + perilaku SQLite) dan Gemini (`GEMINI_CLI_HOME` vs API key, lokasi kredensial).
- Bila spike lulus: `credential_strategy` per preset (`token-env` | `home-per-account` | `api-key-env`), `ConfigDirEnv` yang benar per preset (memperbaiki `AgentEnv` yang selalu memakai `CLAUDE_CONFIG_DIR`), daftar fallback per role/rig, pola deteksi limit per provider, dan `gt handoff --agent`.
- `gt doctor` memverifikasi symlink home per akun (I3).

**Di luar scope:** perubahan pada claude-swap; kapasitas per akun di scheduler (E02); dukungan Linux.

**Acceptance criteria tambahan (4a):**

- [ ] Dua polecat berjalan bersamaan dengan akun Claude berbeda. Keduanya memakai `~/.claude` dan `~/.claude.json` yang sama: skill, rule, hook, dan MCP yang sama tersedia di kedua sesi.
- [ ] Tidak ada direktori config baru yang dibuat Gas Town. Isi `~/.claude` tidak berubah oleh operasi akun (dibandingkan sebelum dan sesudah, kecuali file yang memang ditulis Claude Code saat sesi berjalan).
- [ ] `cswap switch` saat polecat berjalan tidak mengubah akun yang dipakai polecat.
- [ ] Rotasi karena limit melanjutkan transkrip yang sama (`--resume`) dengan akun lain.
- [ ] Token tidak muncul di `ps`, environment tmux global, `accounts.json`, maupun log.

## 8. Yang belum diverifikasi

- Perilaku `CLAUDE_CODE_OAUTH_TOKEN` saat Keychain berisi login akun lain (dokumen menyatakan token menang; belum diuji di mesin ini).
- Apakah pesan usage-limit di pane sama untuk sesi `setup-token`.
- Apakah `--resume` sesi yang dibuat dengan akun A berjalan normal dengan token akun B.
- Codex: penempatan file WAL/SHM untuk DB yang di-symlink, dan apakah keyring Codex terikat ke `CODEX_HOME`.
- Gemini: lokasi kredensial pada versi yang terpasang.

Semua butir ini masuk spike awal Tahap 2. Satu percobaan listing nama item Keychain diblokir oleh classifier keamanan, sehingga skema nama entri Keychain hanya dikonfirmasi lewat kode (claude-swap dan Gas Town), tidak lewat mesin ini.

## 9. Pertanyaan baru

**Jawaban (12 Oktober 2026):**

- **Q12: tidak butuh** connectors/Remote Control → **opsi A final** untuk 4a.
- **Q10: login berlangganan** → 4b memakai strategi `home-per-account` (bukan API key). Spike Codex (SQLite/WAL di home yang di-symlink) dan Gemini (lokasi kredensial) **wajib** sebelum kode. Effort 4b tetap M–L.

- **Q12 — Batasan token per proses.** Apakah sesi agent Gas Town membutuhkan claude.ai connectors (MCP yang ditambahkan lewat claude.ai, bukan lewat `claude mcp add`) atau Remote Control?
  - Default: **tidak**, sehingga opsi A dipakai.
  - Jika ya, 4a beralih ke opsi C (home per akun dengan denylist). Effort naik ke M–L, dan `.claude.json` per akun perlu sinkronisasi.
- **Q10 (dipertajam).** Untuk provider fallback, apakah Anda memakai login berlangganan (ChatGPT/Google) atau API key?
  - API key memenuhi I1 penuh tanpa home per akun.
  - Login berlangganan butuh home per akun dan spike SQLite (Codex).
