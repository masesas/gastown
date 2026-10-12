# S1b — State Fail-Closed di Boundary Lifecycle

Asal: Rank 5 / E16 slice (a)–(c) (`../09` § 4). Tahap 1. Harus di-merge sebelum S2b.

## 1. Masalah

Tiga titik memperlakukan "tidak tahu" sebagai "aman untuk bertindak":

1. Reaper membunuh sesi baru yang sehat karena membaca heartbeat lama milik sesi sebelumnya (#4890 [U]).
2. Lookup "apakah polecat masih punya pekerjaan" yang gagal dianggap "tidak ada pekerjaan", sehingga worker sehat di-reap.
3. Sesi lama yang bead-nya sudah di-re-sling ke polecat lain tetap bisa menjalankan `gt done` dan mengirim MR ganda.

## 2. Perilaku yang diharapkan

### 2.1 Heartbeat ber-generation

- Setiap heartbeat menyimpan identitas sesi (session id/generation).
- Saat sesi baru dimulai, heartbeat lama dihapus atau ditulis ulang dengan generation baru **sebelum** menunggu runtime siap.
- Reaper hanya memakai heartbeat yang generation-nya sama dengan sesi tmux yang sedang hidup. Heartbeat dari generation lain diperlakukan sebagai "belum ada heartbeat", bukan "basi". Bila heartbeat generation lain sudah berumur ≥ 3× timeout reaper (sesi baru tidak pernah menulis heartbeat), reaper memakai jalur pemeriksaan "working" yang ada: kill hanya bila tidak ada pekerjaan ter-assign dan agent tidak hidup.
- Heartbeat yang tidak bisa dibaca (error I/O atau parse) diperlakukan sebagai `unknown`, bukan nil.
- Heartbeat format lama (tanpa generation) tetap terbaca; diperlakukan seperti sekarang agar upgrade tidak membunuh sesi yang sedang berjalan.

### 2.2 Lookup assignment tri-state

- Pertanyaan "apakah agent X punya pekerjaan terbuka" menghasilkan `yes`, `no`, atau `unknown`.
- `unknown` (error store, timeout) **tidak pernah** memicu kill, nuke, atau cleanup. Reaper mencatat event `state_unknown` dan mencoba lagi di siklus berikutnya.
- Bila `unknown` berlangsung lebih dari ambang (default 3 siklus berturut-turut), daemon mengirim eskalasi, bukan kill.

### 2.3 Guard kepemilikan di `gt done`

- Sebelum push atau membuat MR, `gt done` memastikan bead di hook masih ter-assign ke agent yang menjalankannya.
- Bila assignee berbeda atau lookup `unknown`: `gt done` gagal dengan exit nonzero dan pesan yang menyebut assignee saat ini (atau alasan unknown). Tidak ada push dan tidak ada MR.
- Override disengaja tidak disediakan di slice ini.

## 3. Acceptance criteria

- [ ] **S1b-AC1** Heartbeat generation lama dengan state `exiting` + sesi baru hidup → reaper tidak membunuh sesi baru.
- [ ] **S1b-AC2** Start sesi menulis ulang heartbeat sebelum menunggu runtime siap.
- [ ] **S1b-AC3** Heartbeat rusak/tidak terbaca → `unknown`, reaper tidak membunuh.
- [ ] **S1b-AC4** Heartbeat format lama tetap terbaca (kompatibilitas).
- [ ] **S1b-AC5** Error `bd` saat lookup assignment → reaper tidak membunuh, event `state_unknown` tercatat.
- [ ] **S1b-AC6** `unknown` berturut-turut melewati ambang → eskalasi terkirim satu kali per episode, tanpa kill.
- [ ] **S1b-AC7** `gt done` oleh agent yang bukan assignee → exit nonzero, tanpa push, tanpa MR.
- [ ] **S1b-AC8** `gt done` saat lookup assignment error → exit nonzero, tanpa push, tanpa MR.
- [ ] **S1b-AC9** Jalur normal (`yes`/`no` jelas, assignee benar) berperilaku sama seperti sebelumnya (test existing tetap hijau).

## 4. Di luar scope

Lease/fencing penuh (E03); refactor seluruh pembacaan state; split-brain multi-socket (E30); deteksi usage limit (S2b).

## 5. Rollout dan rollback

Tiga perubahan (2.1, 2.2, 2.3) dikirim sebagai PR terpisah, bisa di-revert masing-masing. Tidak ada flag config; perubahan bersifat koreksi.

## 6. Metrik

False kill (sesi di-kill tetapi bead-nya kemudian selesai normal oleh sesi berikutnya); re-sling manual per minggu; jumlah event `state_unknown`.

## Riwayat

- 12 Okt 2026: dibuat dari `09` Rank 5. Ditambahkan: kompatibilitas heartbeat lama (AC4), eskalasi saat unknown berkepanjangan (AC6), `gt done` saat unknown (AC8).
- 12 Okt 2026: aturan orphan untuk heartbeat generation lain (≥ 3× timeout) dari verifikasi kode.
