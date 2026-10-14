# Aset S1c — Koordinasi Contract-First

Sumber terversi untuk formula, directives, dan template S1c. Tidak ada kode Go. Spec: `../spec/S1c-koordinasi-prosedural.md`. Plan: `../plan/P-S1c-koordinasi-prosedural.md`.

## Isi

| File | Dipasang ke |
| --- | --- |
| `formulas/mol-contract-brief.formula.toml` | `<town>/.beads/formulas/` |
| `formulas/mol-contract-brief-review.formula.toml` | `<town>/.beads/formulas/` |
| `formulas/mol-contract-work.formula.toml` | `<town>/.beads/formulas/` |
| `directives/town/mayor.md` | `<town>/directives/mayor.md` |
| `directives/rig/crew.md` | `<town>/<rig>/directives/crew.md` (setiap rig yang punya crew `contract_owner`) |
| `directives/rig/polecat.md` | `<town>/<rig>/directives/polecat.md` |
| `templates/*.md` | `<town>/templates/contract/` (dirujuk sebagai `$GT_ROOT/templates/contract/`; agent town tidak bisa membaca repo fork) |

## Pemasangan (P-S1c Langkah 5)

```bash
RIGS="posin"   # fase 1: satu rig monorepo; fase 2: "be pro"
cp formulas/*.formula.toml "$TOWN/.beads/formulas/"
mkdir -p "$TOWN/templates/contract" && cp templates/*.md "$TOWN/templates/contract/"
mkdir -p "$TOWN/directives" && cp directives/town/mayor.md "$TOWN/directives/mayor.md"
for rig in $RIGS; do
  mkdir -p "$TOWN/$rig/directives"
  cp directives/rig/crew.md "$TOWN/$rig/directives/crew.md"
  cp directives/rig/polecat.md "$TOWN/$rig/directives/polecat.md"
done
```

Bila file directives sudah ada di town, gabungkan secara manual; jangan menimpa.

Verifikasi: `gt formula run <nama> --dry-run --rig <rig>` untuk ketiga formula, lalu `gt prime --explain` untuk Mayor, crew, dan polecat.

Catatan bd 1.3.0: tidak ada tipe `question` dan tidak ada `bd gate close`. Bead bertipe `gate` disembunyikan dari `bd list`, sehingga `gt convoy stage` tidak melihatnya dan tidak menahan dependennya (diuji 14 Okt; gt-pd1c.43). Karena itu bead brief dan bead pertanyaan memakai tipe `decision`, dibuat sebagai anak epic, dan ditutup dengan `bd close <id> --reason ...`. Bead brief sekaligus menjadi gate persetujuan.

## Rollback

Hapus tiga file formula, folder `templates/contract`, dan file directives yang dipasang di atas. Tidak ada perubahan lain di town.
