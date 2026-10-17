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
- Pre-push guard Gas Town (gt-pd1c.72) terpasang di repo rig: `<rig>/.repo.git/hooks/pre-push` (berlaku untuk refinery dan semua worktree polecat), `mayor/rig` dan clone crew. Dipasang otomatis oleh `gt rig add`, `gt up`, pembuatan polecat/crew; cek dengan `gt doctor` (check `rig-prepush-guard`), perbaiki dengan `gt doctor --fix`. Hook memanggil `gt hooks pre-push`: agent (env `GT_ROLE`/`BD_ACTOR`/`GT_POLECAT`/`GT_CREW`) ditolak saat push yang me-land integration branch ke `main`/`integration/*` bila `integration_branch_auto_land` bukan `true`, `GT_INTEGRATION_LAND=1` tidak berlaku untuk agent; setiap push agent ke `main`/`integration/*` juga diperiksa `allowed_paths`. Push `polecat/*` tidak diperiksa. Aturan koordinator (gt-pd1c.78): penghapusan `polecat/*` oleh agen selain crew hanya boleh bila tidak ada kerja yang hilang (tip sudah ada di `main`/`integration/*` di remote, atau merge queue menunjukkan tidak ada MR terbuka dan tidak ada yang landed, atau MR yang landed menurut bukti git dan commit-nya sama dengan tip; vonis landed yang hanya dari `close_reason`/`merge_commit` atau vonis manusia tidak dipercaya). Rewrite (bukan fast-forward) `polecat/*` oleh agen selain crew, refinery dan polecat pemiliknya diperiksa dengan aturan yang sama terhadap tip lama. MR terbuka, landing yang tak bisa diputuskan git, atau beads tak terbaca = ditolak. Update `main`/`integration/*` hanya oleh refinery, crew dan manusia (polecat hanya bila `merge_queue.enabled=false`; pembuatan `integration/*` tanpa commit baru boleh oleh agen mana pun). `gt polecat nuke --force` memakai aturan penghapusan yang sama; selain itu worktree harus bersih (tanpa file uncommitted/untracked yang tidak diabaikan dan tanpa stash), tanpa hook bead aktif dan tanpa kerja terbuka yang masih di-assign; bila tidak terpenuhi, agen ditolak dan manusia/crew harus mengetik ulang alamat polecat di terminal. Guard lokal bisa dilewati; jaminan nyata hanya proteksi branch atau pre-receive di remote. Tanpa `gt` di PATH semua push ditolak. Hook pre-push milik proyek di `.git/hooks` dipertahankan sebagai `pre-push.gt-chained` dan dijalankan sesudahnya; repo yang memakai `core.hooksPath` milik proyek harus memanggil `gt hooks pre-push` sendiri (bila tidak, `gt doctor` melaporkan konflik).
- Aturan tambahan: push agent ke `main`/`integration/*` dari repo ber-hook terkelola yang rig-nya tidak terdeteksi ditolak (rig dibaca dari path nyata, symlink di-resolve; repo tanpa hook terkelola di luar rig, mis. repo source Gas Town, tetap dicek tanpa settings rig); update agent ke `main`/`integration/*` yang bukan fast-forward (force push/rewind) ditolak; agent tidak boleh menghapus default branch, dan hanya boleh menghapus `integration/*` yang sudah land di default branch remote; bila `default_branch` rig tidak terbaca, agent memperlakukan `main`, `master` dan `origin/HEAD` sebagai default branch.
- Batas: hook lokal bisa dilewati (`git push --no-verify`, `git -c core.hooksPath=... push`, menghapus hook, atau membuang env identitas agent). Squash merge, cherry-pick atau rebase isi integration ke `main` membuat SHA baru dan tidak terdeteksi sebagai land (hanya dicek scope). `GT_INTEGRATION_LAND=1` hanya penanda kebijakan: dengan auto_land=true agent mana pun yang menyetelnya bisa land lewat push mentah. Tanpa `allowed_paths`, agent mana pun (termasuk polecat) bisa push perubahan non-land langsung ke `main`/`integration/*`; polecat tidak ditolak total karena strategi merge convoy `direct` membuat `gt done` push langsung ke target. Maka isi `allowed_paths` dan pasang branch protection. Perlindungan terkuat tetap remote: rig hanya ke mirror (di atas) dan branch protection di origin asli, promosi lewat PR yang di-review manusia.
- Bukti sesudah `gt rig add`: `gt doctor` menunjukkan `rig-prepush-guard` OK; dari `<rig>/refinery/rig`, `GT_ROLE=refinery GT_INTEGRATION_LAND=1 git push origin <branch-yang-memuat-integration>:main` ditolak dengan pesan `BLOCKED: ... lands integration branch ...`.

## 3. Gate yang membuktikan

- `test_command` rig wajib menjalankan tes integrasi terhadap DB sungguhan. Contoh: `assets/tools/posin-gate`. Script itu menyalakan PostgreSQL dan Redis sekali pakai (tmpfs, port localhost acak), menerapkan semua migrasi, menjalankan vet dan tes dengan `TEST_DATABASE_URL`/`TEST_REDIS_URL`, menggagalkan gate bila ada tes yang di-skip karena env DB kosong, lalu menghapus container.
- Uji negatif sekali sebelum epic pertama: rusak satu query di branch percobaan, lalu pastikan gate gagal.
- `pre_verified` tidak pernah menggantikan gate refinery (PR #20, #27). Periksa log refinery: gate jalan untuk setiap MR.
- Container dev milik proyek lain jangan dipakai untuk gate.
- Batasi path yang boleh diubah MR dengan `merge_queue.allowed_paths` di `<rig>/settings/config.json`, mis. `"allowed_paths": ["apps/**", "contracts/openapi.yaml"]` (kunci tidak ada atau `[]` = tanpa batas). Penegakan keras: saat submit `gt done`/`gt mq submit` menolak branch yang menyentuh path lain (mis. `.brief-reviews/...` dari leg review, kejadian 16 Okt); saat push, pre-push guard Gas Town (§ 2) menolak push agent ke `main`/`integration/*` yang rentang `remote_sha..local_sha`-nya keluar scope, sehingga refinery yang keluar dari formula (pilot: `git push origin temp-mr:<target>` + `bd close` mentah) kini tertahan selama hook tidak dilewati. Pemeriksaan saat merge tetap instruksi formula: refinery diminta menjalankan `gt mq check-scope <rig> <mr-id>`, me-merge persis `Checked-SHA`, dan mengirim FIX_NEEDED bila melanggar. Pola invalid membuat seluruh settings rig gagal dimuat juga untuk caller lain. Symlink di dalam path yang diizinkan yang menunjuk keluar tetap lolos (dinilai dari path di tree).
- Leg review (formula convoy dengan `review_only = true`, mis. `mol-contract-brief-review`) dibuat dengan field `review_only: true` di bead leg. Mode ini melekat: `gt sling <leg> <rig>` ulang tanpa `--review-only` (juga `--force`, dispatch scheduler, setelah polecat di-nuke) tetap review-only dan memperbarui `attached_at`. Sling yang gagal (hook/spawn) di-rollback ke keadaan sebelum sling itu: bila bead sebelumnya bukan review-only, marker yang ditambahkan sling tersebut dihapus; bila sebelumnya review-only, keadaan awal tak diketahui, atau penulisan rollback gagal, marker tetap (fail-closed). `gt done` dan `gt mq submit` menolak commit/MR bila bead target (`--issue` atau branch) atau assignment aktif agen pemanggil review-only. Assignment aktif dipilih sama seperti `gt done` memilih hook: bead `hooked` menang atas `in_progress` (leg review yang sudah diklaim `in_progress` tetap terhitung; leg basi `in_progress` kalah dari hook saat ini); bila pilihan ambigu semua kandidat dihitung. Gagal lookup atau identitas polecat tak terbaca = ditolak. `gt done` tidak auto-commit file sisa leg review; leg yang hanya melapor selesai dengan komentar assignee `bd comments add <leg> "FINDINGS: head_sha=$(git rev-parse HEAD) <temuan>"` (token `head_sha` boleh di mana pun dalam baris) tanpa commit baru, dan pesan penolakan menyebut pemulihan `git reset --hard <base>` (kejadian kp-leg-qoqr6, 16 Okt). Commit di branch leg review tidak pernah di-push untuk "dipertahankan": `gt polecat nuke`, penghapusan worktree polecat dan `gt doctor --fix` melewati push, dan checkpoint dog tidak membuat commit WIP pada leg review; yang dicek bead di nama branch dan bead hook polecat (kejadian kp-aqs). Bila status tak terbaca, push/checkpoint juga dilewati tetapi nuke dan pembersihan branch basi mempertahankan branch lokal (commit tetap ada di repo rig), dan `gt doctor --fix` melaporkan push sebagai ditunda. Leg yang dibuat sebelum perubahan ini belum punya field tersebut: sling ulang dengan `gt sling <leg> <rig> --review-only` (field ditulis di blok header deskripsi). Field hanya dibaca dari blok header deskripsi (baris-baris awal `key: value` sampai baris kosong atau blok Markdown pertama); baris `review_only: true` di badan bebas, blok kode atau kutipan diabaikan dan `gt sling` memberi peringatan. Nilai hanya `true`/`false`; nilai lain (`no`, `off`, `0`, `maybe`) tetap dianggap review-only (fail-closed), tidak dikanonisasi diam-diam, dan `gt sling` memberi peringatan. Melepas marker: `gt bead clear-review-only <leg> --reason "<alasan>"` (hanya manusia/overseer, crew atau mayor; polecat ditolak), tercatat sebagai komentar bead (siapa, kapan, alasan) dan event `review_only_cleared`. Asimetri: `no_merge` adalah field assignment seperti `attached_molecule` (ikut terhapus saat molekul di-detach/burn, mis. pada sling ulang formula; dipulihkan ke nilai sebelumnya saat rollback; tidak ditambahkan lagi tanpa `--no-merge`), sedangkan `review_only` melekat pada bead (bertahan pada detach, burn dan setiap assignment berikutnya) sampai dilepas eksplisit.

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
