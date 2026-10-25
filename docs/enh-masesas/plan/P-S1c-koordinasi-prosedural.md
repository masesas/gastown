# Impl Plan P-S1c — Koordinasi Contract-First (Prosedural)

Spec: [`../spec/S1c-koordinasi-prosedural.md`](../spec/S1c-koordinasi-prosedural.md). Tanpa kode Go. Artefak: dua formula baru, directives, dan prosedur pilot. Prasyarat: Tahap 0 (crew `contract_owner` ada di setiap rig pilot).

**Status (15 Okt 2026):** Langkah 1–9 selesai di pilot fase 1 (rig monorepo `posin`), epic pilot `kp-rcu`. Hasil dan temuan: [`../pilot/epic-1-s1c.md`](../pilot/epic-1-s1c.md). Tersisa baseline epic manual B (`../baseline/metrics-baseline.md`) sebelum keputusan gate Tahap 1.

## 0. Fakta mekanisme (dikonfirmasi dari kode)

| Mekanisme | Fakta | Dampak ke rencana |
| --- | --- | --- |
| Formula baru | Cukup file `<nama>.formula.toml` di `<town>/.beads/formulas/` (`findFormulaFile`, `internal/cmd/formula.go:1039-1070`). Overlay hanya mengubah step formula existing | Dua formula baru, bukan overlay |
| Provisioning embed | Tidak menimpa file yang sudah ada (`internal/formula/embed.go:173`) | Nama baru aman |
| Validasi | Tidak ada `gt formula validate`; `gt formula run <nama> --dry-run --rig R` menjalankan parser + `Validate()` (`parser.go:60-83`) | AC1 via dry-run |
| Workflow | `[[steps]]` dengan `needs` (harus merujuk step sebelumnya), `target` (`""`/`rig`/`mayor`/…, ditulis sebagai `workflow_target`), `interactive` (bila ada satu step interaktif, semua step siap di-hook ke sesi saat ini) | Hindari `interactive`; step Mayor pakai `target = "mayor"` |
| Convoy | `[[legs]]` paralel + `[synthesis]`; leg di-sling dengan `--no-convoy [--review-only]` | Review brief = convoy formula terpisah |
| Directives | Town `<town>/directives/<role>.md`, rig `<town>/<rig>/directives/<role>.md`, digabung town lalu rig; Mayor hanya membaca level town (`config/directives.go:19`, `prime_output.go:120-174`) | Directives per role, bukan per nama crew |
| Penahanan dependen | Blocker terbuka yang tidak slingable menahan dependen di `convoy stage` (`computeWaves`, `convoy_stage.go:1003`) **hanya bila blocker ikut terambil** ke DAG. Bead bertipe `gate` disembunyikan `bd list`, sehingga tidak terambil dan tidak menahan (diuji Langkah 4; gt-pd1c.43). Tipe `decision` terambil dan menahan | Bead brief (`decision`) sekaligus gate persetujuan; bead pertanyaan `block` juga `decision`; semuanya anak epic |
| Auto-close stale | `wisp_reaper` menutup bead diam ≥ 7 hari kecuali label `gt:keep` (`reaper.go:721-753`) | Semua bead gate, brief, dan pertanyaan diberi `gt:keep` |

## 1. Artefak dan lokasi sumber

Pilot memakai salinan manual dari `docs/enh-masesas/assets/` (Langkah 5). Sejak 25 Okt 2026 aset ikut binary dan dipasang otomatis (lihat `../assets/README.md`):

```
internal/formula/formulas/                    # → <town>/.beads/formulas/ (gt install)
├── mol-contract-brief.formula.toml           # workflow: draf → review → cek → approval; memuat template brief, cek mekanis, pertanyaan
├── mol-contract-brief-review.formula.toml    # convoy: leg per area konsumen + sintesis
└── mol-contract-work.formula.toml            # workflow crew contract_owner
internal/directives/defaults/
├── town/mayor.md        # → <town>/directives/mayor.md (gt install)
├── rig/crew.md          # → <town>/<rig>/directives/crew.md (gt rig add; crew = contract_owner)
└── rig/polecat.md       # → <town>/<rig>/directives/polecat.md (gt rig add)
```

Spec menyebut dua formula; review dipisah menjadi formula convoy ketiga karena leg paralel hanya tersedia di tipe convoy.

## 2. Rancangan formula

### 2.1 `mol-contract-brief` (workflow)

| Step | `target` | `needs` | Isi instruksi (ringkas) | Output yang dicek |
| --- | --- | --- | --- | --- |
| `draft` | `mayor` | — | Susun brief dari `templates/brief.md` ke field `design` bead brief (`gt:brief`, `gt:keep`). Isi: Kontrak, Peta kepemilikan, Konsumen, Keputusan, Pertanyaan & asumsi. Buat bead skeleton (assignee crew `contract_owner`) dan bead implementasi shared; pasang `bd dep add <konsumen> <skeleton>` | Bead brief terisi, dependensi ada |
| `review` | `mayor` | `draft` | Jalankan `gt formula run mol-contract-brief-review --set brief=<id> --set round=<n>`; tunggu sintesis. Ronde 2 hanya bila ada `gap`/`conflict`; revisi brief di antaranya. Maksimal 2 ronde | File sintesis review + verdict per bead |
| `check` | `mayor` | `review` | Isi `templates/brief-check.md` ke notes bead brief. Setiap butir disertai bukti perintah (`bd dep list`, `bd show`, `git ls-remote` untuk nomor migrasi di `epic/*`) | Checklist lengkap, semua lulus |
| `decide` | `mayor` | `check` | Sisa `conflict` → bead pertanyaan (§ 3). Tutup bead gate approval: `bd gate close <gate> --reason "brief <id> approved: review r<n>, check ok"` | Gate tertutup; daftar pertanyaan |

Bead brief (`--type=decision --labels gt:brief,gt:keep --parent <epic>`) dibuat Mayor saat `draft` dan menjadi gate persetujuan: konsumen bergantung padanya, sehingga tertahan sampai brief ditutup (disetujui) **dan** skeleton merged. Tidak ada bead gate terpisah (lihat hasil Langkah 3–4).

### 2.2 `mol-contract-brief-review` (convoy)

- `review_only = true`. Input: `brief` (id bead), `round`. `gt formula run` menulis field `review_only: true` ke setiap bead leg saat dibuat, sehingga sling ulang leg tetap review-only (gt-pd1c.69).
- Legs: `be-consumer` dan `mobile-consumer` (area pilot). Leg tambahan ditambah di formula bila area bertambah.
- Prompt dasar setiap leg berisi pertanyaan tetap spec § 2.2 dan memaksa output terstruktur `{bead, verdict: ok|gap|conflict, items[]}` (blok JSON di file leg).
- Sintesis menggabungkan verdict per bead; tidak memutuskan.

### 2.3 `mol-contract-work` (workflow, untuk crew `contract_owner`)

| Step | Isi |
| --- | --- |
| `skeleton` | Implementasi skeleton kontrak sesuai brief + commit `docs/briefs/<epic>.md`; PR ke `epic/<nama>` lewat jalur refinery biasa |
| `pin` | Setelah merged: tulis `contract_ref: <repo>:<path>@<sha>` ke notes bead skeleton dan setiap bead konsumen; mail ke Mayor |
| `shared` | Implementasi shared (bead terpisah, tidak memblokir) |
| `contract-change` | Saat ada bead `contract-change`: revisi kontrak, `contract_ref` baru, mail Mayor dan konsumen terkait |

## 3. Directives

### `town/mayor.md` — isi wajib (S1c-AC2)

1. Kriteria briefing wajib (hot path / kontrak lintas-rig / ≥ 3 bead paralel di satu rig) → jalankan `mol-contract-brief`.
2. Larangan: menyetujui brief tanpa review + checklist; memutuskan hal krusial; menganggap diam sebagai setuju; `gt sling` manual untuk bead konsumen (pakai `gt convoy stage` + `launch`).
3. Tabel krusial dan kelas (`../14` § 2.2), disalin utuh.
4. Pembuatan bead pertanyaan dari `templates/question.md`: tipe non-slingable (`question` bila didukung bd terpasang, selain itu `gate`), label `gt:question,gt:keep`, field `id/class/question/context/options/assumed/change_seam/affected/rework_cost` di deskripsi.
5. `block`: `bd dep add <task> <question>`; `gt mail send --human -s "[BLOCK] Q-<epic>-<n>: <ringkas>" --stdin`; pengingat 24 jam (Mayor memeriksa saat patrol/handoff; otomatis di S3).
6. `proceed`: `bd label add <task> assumes:Q-<epic>-<n>`; catatan asumsi di notes; propagasi saat dispatch: salin semua label `assumes:*` dari blocker yang sudah selesai, perbarui `affected`.
7. Pengiriman daftar `proceed` sebelum PR epic → master; format jawaban `Q-id: <jawaban>`.
8. Rework: sama → hapus label, tutup pertanyaan; beda → bead rework per bead bertanda (`discovered-from`, dependensi disalin, kontrak dulu).
9. Larangan membuat PR epic → master selama ada label `assumes:*` dengan pertanyaan terbuka atau rework belum merged.

### `rig/crew.md` (contract_owner)

Satu-satunya penulis path dalam peta kepemilikan; kerjakan `mol-contract-work`; skeleton dulu; jangan mengerjakan bead konsumen.

### `rig/polecat.md`

Jangan ubah path milik owner (lihat brief); kontrak kurang → buat bead `contract-change` dan berhenti di bagian terdampak; bila bead berlabel `assumes:*`, tulis bagian "Asumsi" di deskripsi PR; baca `contract_ref` di notes.

## 4. Langkah eksekusi

1. **Tulis artefak** di `docs/enh-masesas/assets/` (PR docs ke fork).
2. **Lint formula**: salin ke town salinan uji; `gt formula run mol-contract-brief --dry-run --rig be`, sama untuk dua formula lain → S1c-AC1.
3. **Verifikasi perintah bd** di town salinan: `bd create --type=gate`, `--type=question`, `bd gate close`, `bd label add`, `bd dep add`. Sesuaikan teks directives dengan flag yang benar.
4. **Uji penahanan**: konsumen yang bergantung pada gate terbuka tidak muncul di wave `gt convoy stage`; muncul setelah gate ditutup dan skeleton ditutup.
5. **Pasang ke pilot town** (salin formula ke `<town>/.beads/formulas/`, directives ke lokasinya). Cek `gt prime --explain` pada Mayor, crew, dan polecat → directives termuat.
6. **Epic pilot BE + mobile** (S1c-AC3..AC6): jalankan alur penuh. Catat bukti: id bead brief, file sintesis review, checklist, waktu tutup gate, waktu merge skeleton, waktu dispatch konsumen. Bersamaan, catat baseline workflow manual (pindahan S0-AC9, Epic A P-S0 Langkah 7): satu epic berkompleksitas sebanding dikerjakan manual oleh Anda, metrik `../11` § 3 diisi ke `../baseline/metrics-baseline.md` berdampingan dengan epic S1c.
7. **Simulasi jawaban** (S1c-AC7): satu jawaban sama, satu berbeda.
8. **Uji rollback** (S1c-AC8) di town salinan: hapus formula dan directives → `gt prime --explain` bersih, epic tanpa briefing berjalan normal.
9. **Laporan Tahap 1 (bagian S1c)** ke `../pilot/epic-1-s1c.md`.

**Hasil Langkah 1–5 (14 Okt 2026, pilot fase 1).** "Town salinan uji" diganti: verifikasi perintah bd di DB `bd` sementara (scratch), dry-run dan uji penahanan di pilot town sendiri dengan hanya Dolt yang hidup (tanpa daemon/agent), memakai epic sekali pakai yang lalu ditutup.

| Langkah | Hasil |
| --- | --- |
| 1 | Aset disesuaikan untuk pilot dan bd 1.3.0: template dipasang ke `$GT_ROOT/templates/contract/` (agent town tidak bisa membaca repo fork); `pin` memakai `--append-notes` (`--notes` menimpa); cabang target = integration branch epic (bukan `epic/*`); leg review per area, bukan per rig; contoh template netral; `consumer_rigs` dipisah titik koma (`--set` memecah koma) |
| 2 | `gt formula run <nama> --dry-run --rig posin` rc=0 untuk ketiga formula (S1c-AC1) |
| 3 | bd 1.3.0: tidak ada tipe `question` (ditolak), tidak ada `bd gate close` (gate ditutup dengan `bd close`/`bd gate resolve`); `bd dep add`, `bd label add`, `--append-notes` berfungsi; `bd ready` menahan dependen gate |
| 4 | `gt convoy stage` **tidak** menahan konsumen yang bergantung pada bead `gate` (gate tidak terambil karena disembunyikan `bd list`; gt-pd1c.43). Dengan brief `decision` sebagai blocker: konsumen dikeluarkan dari wave + warning "gated by non-slingable blocker"; setelah brief ditutup, konsumen masuk wave sesudah skeleton. Desain diubah: brief = gate persetujuan, pertanyaan = `decision`, semua anak epic |
| 5 | Terpasang di pilot (`.beads/formulas/`, `templates/contract/`, `directives/mayor.md`, `posin/directives/{crew,polecat}.md`). `gt prime --dry-run --explain`: directives termuat untuk Mayor (town), crew `contract_owner` dan polecat (rig) |

Catatan staging: `gt convoy stage <epic>` tanpa `--no-validate` membuat bead validasi `hq-*` (tipe task) yang tidak terpetakan ke rig dan membuat restage gagal (`[no-rig]`). Untuk cek mekanis, pakai `--no-validate`.

## 5. Pemetaan AC → bukti

| AC | Bukti |
| --- | --- |
| S1c-AC1 | Output dry-run tiga formula (Langkah 2: rc=0) |
| S1c-AC2 | Diff directives terhadap daftar § 3; termuat di prime (Langkah 5) |
| S1c-AC3 | Bead brief (ditutup = disetujui), sintesis review, checklist |
| S1c-AC4 | Query timestamp: `closed_at` skeleton < dispatch setiap konsumen |
| S1c-AC5 | `git log --format='%an %s' epic/<nama> -- <path owner>` |
| S1c-AC6 | `bd list --label gt:question`; `bd list --label assumes:Q-…`; mail `[BLOCK]` |
| S1c-AC7 | Bead rework + label yang terhapus |
| S1c-AC8 | Log uji rollback |

## 6. Batasan eksplisit

- **Out of scope:** semua otomasi Go (S3): guard sling, feed fail-closed, `contract_ref` otomatis, `contract-stale`, guard kepemilikan, pengingat, propagasi otomatis, blokir PR epic.
- **Known edge case:** routing step ke crew lewat `workflow_target` belum teruji; fallback: Mayor men-sling bead skeleton dengan `gt sling <bead> <rig> --crew contract_owner`.
- **Known edge case:** directives `crew.md` berlaku untuk semua crew di rig; aman selama hanya ada crew `contract_owner`.
- **Known limitation:** kepatuhan Mayor bergantung pada LLM; diukur lewat audit sampel dan diperkuat di S3.
