# Aset S1c — Koordinasi Contract-First

Sumber terversi untuk formula, directives, dan template S1c. Tidak ada kode Go. Spec: `../spec/S1c-koordinasi-prosedural.md`. Plan: `../plan/P-S1c-koordinasi-prosedural.md`.

## Isi

| File | Dipasang ke |
| --- | --- |
| `formulas/mol-contract-brief.formula.toml` | `<town>/.beads/formulas/` |
| `formulas/mol-contract-brief-review.formula.toml` | `<town>/.beads/formulas/` |
| `formulas/mol-contract-work.formula.toml` | `<town>/.beads/formulas/` |
| `directives/town/mayor.md` | `<town>/directives/mayor.md` |
| `directives/rig/crew.md` | `<town>/<rig>/directives/crew.md` (be, pro) |
| `directives/rig/polecat.md` | `<town>/<rig>/directives/polecat.md` (be, pro) |
| `templates/*.md` | Dirujuk dari formula dan directives; tetap di repo fork |

## Pemasangan (setelah pilot town ada, P-S1c Langkah 5)

```bash
cp formulas/*.formula.toml "$TOWN/.beads/formulas/"
mkdir -p "$TOWN/directives" && cp directives/town/mayor.md "$TOWN/directives/mayor.md"
for rig in be pro; do
  mkdir -p "$TOWN/$rig/directives"
  cp directives/rig/crew.md "$TOWN/$rig/directives/crew.md"
  cp directives/rig/polecat.md "$TOWN/$rig/directives/polecat.md"
done
```

Bila file directives sudah ada di town, gabungkan secara manual; jangan menimpa.

Verifikasi: `gt formula run <nama> --dry-run --rig be` untuk ketiga formula, lalu `gt prime --explain` untuk Mayor, crew, dan polecat.

## Rollback

Hapus tiga file formula dan file directives yang dipasang di atas. Tidak ada perubahan lain di town.
