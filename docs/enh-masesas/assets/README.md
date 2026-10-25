# Aset S1c — Koordinasi Contract-First

Formula, directives, dan template S1c ikut binary `gt`; tidak ada langkah salin manual. Spec: `../spec/S1c-koordinasi-prosedural.md`. Plan: `../plan/P-S1c-koordinasi-prosedural.md`.

## Isi dan tujuan pemasangan

| Sumber di repo | Dipasang ke | Oleh |
| --- | --- | --- |
| `internal/formula/formulas/mol-contract-brief.formula.toml` | `<town>/.beads/formulas/` | `gt install` |
| `internal/formula/formulas/mol-contract-brief-review.formula.toml` | `<town>/.beads/formulas/` | `gt install` |
| `internal/formula/formulas/mol-contract-work.formula.toml` | `<town>/.beads/formulas/` | `gt install` |
| `internal/directives/defaults/town/mayor.md` | `<town>/directives/mayor.md` | `gt install` |
| `internal/directives/defaults/rig/crew.md` | `<town>/<rig>/directives/crew.md` | `gt rig add` (juga `--adopt`) |
| `internal/directives/defaults/rig/polecat.md` | `<town>/<rig>/directives/polecat.md` | `gt rig add` (juga `--adopt`) |
| `tools/posin-gate` (folder ini) | tidak dipasang | Contoh `test_command` rig dengan DB sekali pakai, spesifik monorepo k-posin dari pilot; salin dan sesuaikan per repo. Lihat `../runbook-real-project.md` § 3 |

Template brief, cek mekanis, dan bead pertanyaan ada di deskripsi step `draft`, `check`, dan `decide` pada `mol-contract-brief`; tidak ada file template di town.

## Perilaku pemasangan

Sama untuk formula dan directives (`internal/assetsync`):

- File hanya ditulis bila belum ada. Hash file yang ditulis dicatat di `.installed.json` di folder tujuan (`.beads/formulas/`, `directives/`, `<rig>/directives/`).
- `gt upgrade` dan `gt doctor --fix` memasang file baru atau yang terhapus, dan memperbarui file yang belum diubah sejak dipasang. File yang diubah lokal dilaporkan (`locally modified`) dan tidak ditimpa.
- Khusus directives: file yang sudah ada tetapi tidak dipasang oleh gt (misalnya salinan manual dari pilot) dianggap milik user dan tidak ditimpa. Bila isinya sama dengan versi binary, file itu dicatat sehingga rilis berikutnya bisa memperbaruinya.
- `gt doctor` cek `formulas` dan `directives` menampilkan status per file.

Verifikasi: `gt formula run <nama> --dry-run --rig <rig>` untuk ketiga formula, lalu `gt prime --explain` untuk Mayor, crew, dan polecat.

Catatan bd 1.3.0: tidak ada tipe `question` dan tidak ada `bd gate close`. Bead bertipe `gate` disembunyikan dari `bd list`, sehingga `gt convoy stage` tidak melihatnya dan tidak menahan dependennya (diuji 14 Okt; gt-pd1c.43). Karena itu bead brief dan bead pertanyaan memakai tipe `decision`, dibuat sebagai anak epic, dan ditutup dengan `bd close <id> --reason ...`. Bead brief sekaligus menjadi gate persetujuan.

## Rollback

Hapus file yang terpasang: tiga file `mol-contract-*.formula.toml` di `<town>/.beads/formulas/`, `<town>/directives/mayor.md`, dan `<town>/<rig>/directives/{crew,polecat}.md`. File yang dihapus tetap tercatat di `.installed.json`, sehingga `gt doctor` melaporkannya `missing` dan `gt doctor --fix` / `gt upgrade` memasangnya lagi. Untuk menonaktifkan directive secara permanen, kosongkan isinya (file kosong tidak dimuat `gt prime` dan dilaporkan sebagai modified, tidak ditimpa).
