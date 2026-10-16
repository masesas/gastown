# Runbook: Gas Town di real project (tahap awal)

Syarat minimum sebelum Gas Town dipakai di repo sungguhan, diturunkan dari pilot S1c (`pilot/epic-1-s1c.md`) dan perbaikan gt-pd1c.50–56. Ruang lingkup: satu rig, paling banyak 2 polecat, operator aktif, land manual.

## 1. Binary dan formula

- Pakai binary dari `main` fork yang memuat PR #25–#32 dan perbaikan G3 (PR #26). Cek dengan `gt version`.
- **Salinan formula di town mengalahkan formula di binary.** `gt install` menyalin formula ke `<town>/.beads/formulas/`. Setiap kali binary diperbarui, bandingkan salinan itu dengan `internal/formula/formulas/` di commit binary:
  ```bash
  for f in <town>/.beads/formulas/*.formula.toml; do
    cmp -s "$f" "internal/formula/formulas/$(basename "$f")" || echo "beda: $f"
  done
  ```
  Salinan yang berbeda hanya karena versi lama boleh ditimpa. Salinan yang punya edit lokal (contoh di pilot: `mol-refinery-patrol` dengan edit hq-6md) harus digabung manual. Simpan backup sebelum menimpa.
- Pasang aset S1c sesuai `assets/README.md` (directives, template, formula contract).

## 2. Remote: rig tidak pernah push ke origin asli

- Remote rig = mirror bare lokal, bukan origin asli:
  ```bash
  git clone --bare <origin-asli> <pilot>/mirror/<repo>.git
  git -C <pilot>/mirror/<repo>.git remote remove origin
  gt rig add <rig> file://<pilot>/mirror/<repo>.git
  ```
- Di clone kerja milik Anda, matikan push ke mirror kecuali saat promosi: `git remote set-url --push <mirror-remote> no_push`.
- Promosi ke origin asli dilakukan manual: dari clone Anda, ambil branch `main` mirror, buat PR ke origin asli, review, merge oleh Anda. `gt mq integration land` hanya mendarat di mirror.
- Bukti sebelum mulai: `git -C <pilot>/mirror/<repo>.git remote -v` kosong, dan percobaan push ke origin asli dari worktree rig gagal.

## 3. Gate yang membuktikan

- `test_command` rig wajib menjalankan tes integrasi terhadap DB sungguhan. Contoh: `assets/tools/posin-gate`. Script itu menyalakan PostgreSQL dan Redis sekali pakai (tmpfs, port localhost acak), menerapkan semua migrasi, menjalankan vet dan tes dengan `TEST_DATABASE_URL`/`TEST_REDIS_URL`, menggagalkan gate bila ada tes yang di-skip karena env DB kosong, lalu menghapus container.
- Uji negatif sekali sebelum epic pertama: rusak satu query di branch percobaan, lalu pastikan gate gagal.
- `pre_verified` tidak pernah menggantikan gate refinery (PR #20, #27). Periksa log refinery: gate jalan untuk setiap MR.
- Container dev milik proyek lain jangan dipakai untuk gate.
- Batasi path yang boleh diubah MR dengan `merge_queue.allowed_paths` di `<rig>/settings/config.json`, mis. `"allowed_paths": ["apps/**", "contracts/openapi.yaml"]`: `gt done`/`gt mq submit` menolak branch yang menyentuh path lain (mis. `.brief-reviews/...` dari leg review, kejadian 16 Okt) dan refinery menjalankan `gt mq check-scope <rig> <mr-id>` sebelum merge lalu mengirim FIX_NEEDED bila ada pelanggaran (merge memakai persis `Checked-SHA` yang diperiksa); kunci tidak ada atau `[]` berarti rig tidak dibatasi. Penilaian memakai path di tree git: symlink di dalam path yang diizinkan yang menunjuk keluar tetap lolos (batas desain).

## 4. Identitas commit

- Isi field `email` di `<town>/mayor/overseer.json`; bila kosong dipakai email global git (`user.email` di config global; config lokal repo diabaikan). Agent commit sebagai author dan committer `<nama-agent>` (witness/refinery: `<rig>/<role>`) dengan email `local+<tag>@domain`, dengan tag = nama agent dalam huruf kecil dan `/` menjadi `-` (mis. `local+nuka@domain`, `local+posin-refinery@domain`). Email overseer yang tidak valid berarti email tidak di-set (PR #28).
- Sesi yang sudah berjalan sebelum binary baru baru memakai identitas baru setelah restart atau handoff.

## 5. Kapasitas

- Batasi polecat di `<town>/settings/config.json`:
  ```json
  { "scheduler": { "max_polecats": 2 } }
  ```
  Nilai N > 0 mengaktifkan dispatch tertunda: `gt sling` dan convoy memberi label pada bead, lalu daemon men-dispatch per heartbeat (batch 1) selama slot tersedia. Nilai -1 (default) atau 0 berarti dispatch langsung tanpa batas. Periksa dengan `gt scheduler status`.
- Pakai satu provider agent per epic. Cek sisa kuota sebelum epic dimulai. Kegagalan spawn karena kuota tidak lagi menghabiskan batas respawn (PR #26), dan dispatch berhenti setelah 5 kegagalan infra berturut-turut. Pulihkan dengan `gt sling respawn-reset <bead>`.
- Bead yang terdampar setelah kegagalan spawn: `gt convoy launch <convoy>` aman diulang (PR #29).

## 6. Idle shutdown

Aktifkan di `<town>/mayor/daemon.json` (gabung dengan isi yang ada, jangan timpa):
```json
{ "patrols": { "idle_shutdown": { "enabled": true, "idle_timeout": "30m" } } }
```
Town di-`gt down` setelah 30 menit tanpa polecat hidup, tanpa bead kerja hooked/in_progress, tanpa MR terbuka, dan tanpa client tmux attach. Wisp patrol, agent bead, dan formula dog infrastruktur tidak dihitung sebagai kerja. Kegagalan membaca bead, MR, atau tmux dianggap aktif. Overseer dikirimi mail sebelum `gt down`. Pengujian launchd di bawah adalah langkah operator, bukan perilaku Gas Town. Alasan town dianggap aktif ditulis di log daemon (`idle_shutdown: town active: ...`). Uji sekali dengan `idle_timeout` pendek, lalu pastikan daemon tidak dinyalakan ulang oleh launchd setelah down. Laptop yang ditinggal tetap sebaiknya di-`gt down` manual (temuan pilot: pilot ikut jalan saat laptop tidur).

## 7. Pengawasan

- Operator aktif selama epic: Monitor dengan alarm stall 15 menit, `gt down` bila macet.
- Epic pertama dibuat kecil dan tidak menyentuh auth, pembayaran, atau migrasi data.
- Satu pertanyaan `block` sengaja dibiarkan terbuka di epic pertama untuk menguji block sempit.
- Target: ≤ 5 intervensi operator per epic. Bila lebih, catat penyebabnya sebagai bead sebelum epic berikutnya.

## 8. Land

- Land integration branch ke `main` mirror hanya setelah: semua bead merged dengan bukti gate, tidak ada `assumes:*` dengan pertanyaan terbuka, dan tes gate lulus di `main` hasil merge.
- Promosi ke origin asli mengikuti § 2.
