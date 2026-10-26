# Baseline Metrik: Workflow Tanpa Orkestrasi vs Gas Town + S1c

Bukti S0-AC9 (dipindah ke P-S1c Langkah 6). Definisi metrik: `../11` § 3. Sampel kecil: angka mentah, median dan rentang bila n > 1, tanpa persentase. Pembanding Gas Town tanpa S1c: `metrics-pilot-s0.md`.

**Perubahan 25 Okt 2026 (hasil /debat):** pembanding B tidak lagi "manual penuh oleh user", melainkan **satu sesi Claude bersama user tanpa orkestrasi** (tanpa Mayor, crew, polecat, refinery). Alasannya: itulah alternatif nyata yang dibandingkan dengan Gas Town, dan metriknya (jam aktif user, intervensi, token Claude) bisa diukur dengan cara yang sama.

## Epic yang dibandingkan

| | Epic tanpa orkestrasi (B) | Epic pilot S1c 1 (A1) | Epic pilot S1c 2 (A2) |
| --- | --- | --- | --- |
| Fitur | Rincian laporan per kategori: field `categories[]` di `ReportSummary` (`GET /api/v1/reports/summary`), bagian "Per Kategori" di layar Laporan | Laporan produk terlaris: `GET /api/v1/reports/products?from&to&limit`, bagian "Produk Terlaris" di layar Laporan | Daftar pelanggan: `GET /api/v1/customers?from&to&q` (turunan `sales_orders`), layar Pelanggan dari tab Lainnya |
| Pengerja | Anda + satu sesi Claude, tanpa orkestrasi (semula: manual penuh) | Gas Town: Mayor (brief), crew `contract_owner` (skeleton), polecat agy/Claude (konsumen) | Gas Town: Mayor (brief), crew `contract_owner` (skeleton + shared), polecat agy-pool (konsumen + rework) |
| Tempat | `~/gt-pilot/manual/k-posin` (clone mirror, push dimatikan) | Rig `posin` di `~/gt-pilot/town`, remote = mirror | sama |
| Titik awal | mirror `main` `9d2b4e9` (selesai di `main` lokal clone `29463a6`) | mirror `main` `0d6368c` | mirror `main` `3340bbd` |
| Kontrak | `contracts/openapi.yaml` + regenerate `internal/httpx/openapi/reports` dan `src/api/schema.d.ts` | sama | `contracts/openapi.yaml` + `api/oapi-codegen/customers.yaml` + regenerate `httpx/openapi/customers` dan `schema.d.ts` |
| Gate | `posin-gate test` (BE `make vet test`; mobile `pnpm typecheck` + `pnpm test --ci`) | sama (refinery; tanpa tes pg saat itu) | sama (refinery; dengan tes pg) |
| Kompleksitas | kontrak + 1 query BE + 1 bagian layar + test; 13 file, +515/−14 | kontrak + 1 query BE + handler + 1 bagian layar + test | kontrak + 1 query SQL + service + handler + 1 layar baru + test; 34 file, +2986/−105 |

## Cara mencatat epic B (sesi Claude bersama Anda)

Catat waktu dalam WIB, cukup sampai menit:

1. **Mulai**: saat sesi mulai membaca kode untuk epic B.
2. **Kontrak siap**: openapi + kode hasil generate di-commit.
3. **Setiap jalannya gate**: waktu, lulus/gagal, penyebab bila gagal. Jalankan `~/gt-pilot/tools/bin/posin-gate test` dari root clone (butuh `source ~/gt-pilot/env.sh` untuk cache).
4. **Selesai**: commit terakhir di `main` lokal clone, gate lulus.
5. Hitung juga: putaran perbaikan (gate gagal → perbaikan), konflik merge, pertanyaan bisnis yang muncul (dan apa yang Anda putuskan), serta waktu aktif Anda (tanpa jeda).
6. **Token Claude** dari transkrip sesi (input + output, cache terpisah) dan **jumlah intervensi Anda** (koreksi arah, jawaban pertanyaan, perbaikan manual).

## Data

| Metrik | Epic B (tanpa orkestrasi) | Epic A1 `kp-rcu` | Epic A2 `kp-4az` |
| --- | --- | --- | --- |
| Lead time mulai → merged ke `main` | 12 m wall-clock (04:28 → 04:40 WIB, 27 Okt), termasuk ±2 m menunggu Docker dinyalakan | 18 j 28 m wall-clock; ±2 j 55 m tanpa jeda semalam (±5 j menunggu keputusan user + ±9 j 20 m laptop tidur + ±1 j 10 m sebelum sesi dilanjutkan), termasuk ±45 m gangguan spawn | 2 j 27 m wall-clock, tanpa jeda (17:14:49 → 19:41:21) |
| Waktu sampai kontrak siap | 5 m (tanpa brief; commit `f8a0bdb` 04:33) | 64 m (brief 53 m + skeleton 10 m) | 52 m (brief 47 m + skeleton 5 m; shared +10 m) |
| Waktu tunggu konsumen terhadap skeleton | n/a | mobile 63 detik, BE 24 detik setelah blocker merged | BE 20 detik, mobile 1 m 45 d setelah blocker ditutup (dispatch convoy 18:20:17) |
| First-pass gate | 1 dari 1: `posin-gate test` lulus pada percobaan pertama (1 m 26 d; BE vet + race + tes pg, mobile typecheck + 1669 tes) | 6 MR merged, tidak teramati gate menolak; gate tidak menjalankan tes pg dan MR mobile lewat pre_verified, keduanya diverifikasi manual operator | 7 MR merged lewat gate dengan tes pg, tidak teramati gate menolak; 1 MR leg review di luar scope ter-merge ke `main` (gate tanpa cek path), direvert |
| Rework loops (dan penyebab) | 0 | 2 bead rework karena jawaban Q-1 berbeda dari asumsi; 0 karena gate | 3 bead rework karena jawaban Q-1 (block) berbeda dari asumsi; 0 karena gate |
| Konflik merge | 0 | 0 | 0 |
| Intervensi manusia | 1 intervensi (menyalakan Docker) + 1 ronde keputusan berisi 4 jawaban bisnis | ±30 intervensi operator + 9 keputusan user (rinci di `../pilot/epic-1-s1c.md` § 4) | 5 intervensi operator + 3 keputusan user (rinci di `../pilot/epic-2-s1c.md` § 4) |
| Pertanyaan krusial / berbeda dari asumsi | 4 (produk tanpa kategori, basis `line_total`, kategori saat ini, urutan + field), ditanyakan sebelum kode / 0 berbeda dari rekomendasi | 6 (1 block, 5 proceed) / 1 dari 5 proceed | 3 (1 block, 2 proceed) / 1 dari 3 (yang block); block sempit: 0 task tertahan |
| Biaya koordinasi (durasi brief; token bila terukur) | tanpa brief. Token sesi (Opus 5.5, 50 panggilan): output 27.447, input 100, cache read 13.648.048, cache write 77.935 — cache read membengkak karena sesi membawa ±270 ribu token konteks dari pekerjaan sebelumnya | 53 m, 2 ronde review; token tidak terukur | 47 m, 2 ronde review; token tidak terukur |
| Waktu aktif manusia | ±3 m (menjawab 4 pertanyaan, menyalakan Docker); user tidak menulis atau mereview kode | tidak terukur (operator = sesi Claude; user menjawab 9 keputusan) | tidak terukur (operator = sesi Claude; user menjawab 3 keputusan) |
| Escaped defects | 0 diketahui (belum ada review kode independen) | 0 diketahui | 0 diketahui pada fitur |

## Catatan

- Epic B, A1, A2 memakai pola, ukuran, dan gate yang sama, tetapi fiturnya berbeda. Perbandingan bersifat indikatif, bukan eksperimen terkontrol.
- Token agy tidak bisa diukur. Token Claude di pilot (Mayor, crew, refinery) belum dikumpulkan; epic pilot berikutnya harus mengumpulkannya dari transkrip agar sebanding dengan B.
- Epic B dijalankan 27 Okt 2026 di `~/gt-pilot/manual/k-posin` oleh satu sesi Claude Code (Opus 5.5) bersama user. Bias yang menguntungkan B: model lebih kuat daripada peran Gas Town (Sonnet/Haiku/agy), sesi sudah membaca baseline ini sehingga fitur sudah terdefinisi, tidak ada review kode independen, dan fitur B lebih kecil daripada A2 (13 vs 34 file). Bias yang merugikan B: tidak ada. Selisih lead time (12 m vs 2 j 27 m) jauh melebihi bias tersebut.
- Penurunan intervensi A1 → A2 sebagian besar hasil perbaikan infrastruktur (G1–G5, agy-pool) di antara dua epic, bukan perubahan proses.
