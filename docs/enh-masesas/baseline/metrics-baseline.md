# Baseline Metrik: Workflow Manual vs Gas Town + S1c

Bukti S0-AC9 (dipindah ke P-S1c Langkah 6). Definisi metrik: `../11` § 3. Sampel kecil: angka mentah, median dan rentang bila n > 1, tanpa persentase. Pembanding Gas Town tanpa S1c: `metrics-pilot-s0.md`.

## Epic yang dibandingkan

| | Epic manual (B) | Epic pilot S1c (A) |
| --- | --- | --- |
| Fitur | Rincian laporan per kategori: field `categories[]` di `ReportSummary` (`GET /api/v1/reports/summary`), bagian "Per Kategori" di layar Laporan | Laporan produk terlaris: `GET /api/v1/reports/products?from&to&limit`, bagian "Produk Terlaris" di layar Laporan |
| Pengerja | Anda, manual (workflow k-posin biasa) | Gas Town: Mayor (brief), crew `contract_owner` (skeleton), polecat (konsumen) |
| Tempat | `~/gt-pilot/manual/k-posin` (clone mirror, push dimatikan) | Rig `posin` di `~/gt-pilot/town`, remote = mirror |
| Titik awal | mirror `main` `0d6368c` | mirror `main` `0d6368c` |
| Kontrak | `contracts/openapi.yaml` + regenerate `internal/httpx/openapi/reports` dan `src/api/schema.d.ts` | sama |
| Gate | `posin-gate test` (BE `make vet test`; mobile `pnpm typecheck` + `pnpm test --ci`) | sama (refinery) |
| Kompleksitas | kontrak + 1 query BE + 1 bagian layar + test | kontrak + 1 query BE + handler + 1 bagian layar + test |

## Cara mencatat epic manual (untuk Anda)

Catat waktu dalam WIB, cukup sampai menit:

1. **Mulai**: saat Anda mulai membaca kode untuk epic B.
2. **Kontrak siap**: openapi + kode hasil generate di-commit.
3. **Setiap jalannya gate**: waktu, lulus/gagal, penyebab bila gagal. Jalankan `~/gt-pilot/tools/bin/posin-gate test` dari root clone (butuh `source ~/gt-pilot/env.sh` untuk cache).
4. **Selesai**: commit terakhir di `main` lokal clone, gate lulus.
5. Hitung juga: putaran perbaikan (gate gagal → perbaikan), konflik merge, pertanyaan bisnis yang muncul (dan apa yang Anda putuskan), serta waktu aktif kira-kira (tanpa jeda).

## Data

| Metrik | Epic manual (B) | Epic pilot S1c (A) |
| --- | --- | --- |
| Lead time mulai → merged ke `main` | | 18 j 28 m wall-clock; ±2 j 55 m tanpa jeda semalam (±5 j menunggu keputusan user + ±9 j 20 m laptop tidur + ±1 j 10 m sebelum sesi dilanjutkan), termasuk ±45 m gangguan spawn |
| Waktu sampai kontrak siap | | 64 m (brief 53 m + skeleton 10 m) |
| Waktu tunggu konsumen terhadap skeleton | n/a | mobile 63 detik, BE 24 detik setelah blocker merged |
| First-pass gate | | 6 MR merged, tidak teramati gate menolak; gate tidak menjalankan tes pg dan MR mobile lewat pre_verified, keduanya diverifikasi manual operator |
| Rework loops (dan penyebab) | | 2 bead rework karena jawaban Q-1 berbeda dari asumsi; 0 karena gate |
| Konflik merge | | 0 |
| Intervensi manusia | n/a (semua manual) | ±30 intervensi operator + 9 keputusan user (rinci di `../pilot/epic-1-s1c.md` § 4) |
| Pertanyaan krusial / berbeda dari asumsi | | 6 (1 block, 5 proceed) / 1 dari 5 proceed |
| Biaya koordinasi (durasi brief; token bila terukur) | n/a | 53 m, 2 ronde review; token tidak terukur |
| Waktu aktif manusia | | tidak terukur (operator = sesi Claude; user menjawab 9 keputusan) |
| Escaped defects | | 0 diketahui |

## Catatan

- Epic B dan A memakai pola, ukuran, dan gate yang sama, tetapi fiturnya berbeda. Perbandingan bersifat indikatif, bukan eksperimen terkontrol.
