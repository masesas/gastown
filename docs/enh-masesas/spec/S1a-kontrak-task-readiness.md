# S1a — Kontrak Task Utuh sampai ke Worker + Readiness

Asal: Rank 1 / E04 (`../09` § 4), ditambah wiring validasi variabel formula (E17). Tahap 1.

## 1. Masalah

Bead yang ditulis lengkap (deskripsi, AC, design, notes) tidak sampai utuh ke agent: `gt prime` hanya menampilkan 5 baris pertama. Bead yang belum siap (tanpa AC) tetap bisa di-dispatch, dan formula dengan variabel yang tidak terdefinisi baru gagal di tengah jalan. Akibatnya agent menebak requirement dan terjadi rework.

## 2. Perilaku yang diharapkan

### 2.1 Render penuh di prime

- `gt prime` untuk agent yang punya bead di hook menampilkan, berurutan: judul, deskripsi penuh, acceptance criteria, design, notes, dan `contract_ref` bila ada (dipakai S3 kelak).
- Bagian yang kosong tidak ditampilkan.
- Ada batas ukuran per bagian dan total, bisa dikonfigurasi. Bila terpotong, ada penanda eksplisit yang menyebut jumlah baris yang tidak ditampilkan dan perintah untuk melihat sisanya (`bd show <id>`).
- Perilaku untuk bead tanpa field tambahan tidak berubah selain deskripsi tidak lagi dipotong 5 baris.

### 2.2 Readiness check

- Berlaku untuk bead tipe `task`, `feature`, `bug` saat di-dispatch lewat `gt sling` maupun scheduler.
- Bead **siap** bila: deskripsi tidak kosong (setelah baris lampiran mesin seperti `attached_formula:` diabaikan), dan AC berisi minimal satu item checklist (`- [ ]` atau `- [x]`). AC berbentuk teks bebas tanpa checkbox dianggap tidak siap.
- Mode per rig `readiness: off | warn | enforce`, default `warn`.

| Mode | Bead tidak siap |
| --- | --- |
| `off` | Dispatch seperti sekarang, tanpa cek |
| `warn` | Dispatch tetap jalan; peringatan berisi alasan ditampilkan; event `task.readiness{result: rejected, reasons}` dicatat dengan `enforced=false` |
| `enforce` | Dispatch ditolak dengan pesan alasan dan exit nonzero (sling) atau bead dilewati dengan alasan `not_ready` (scheduler) |

- `gt sling --force-unready --reason "<teks>"` melewati cek di mode `enforce`. Flag ini terpisah dari `--force`. Actor dan alasan dicatat sebagai komentar bead (tidak menimpa notes) dan event `task.readiness{result: overridden}`. `--force-unready` tanpa `--reason` ditolak.
- Kegagalan membaca bead saat cek readiness diperlakukan sebagai **tidak siap** (fail-closed) di mode `enforce`, dan peringatan di mode `warn`.

### 2.3 Validasi variabel formula

- Sebelum formula di-cook/diinstansiasi saat sling, semua `{{var}}` yang dipakai formula harus terdefinisi (dari `--var`, default formula, atau variabel bawaan).
- Validasi dilakukan atas formula yang sudah di-resolve (rig → town → embedded, termasuk rantai extends/compose).
- Mode mengikuti setting readiness rig: `enforce` → sling gagal sebelum ada efek samping, dengan pesan yang menyebut nama variabel; `warn` → peringatan dan event; `off` → tanpa cek. Alasan: formula user yang berjalan hari ini bisa terkena false positive, jadi tidak langsung diblokir.

## 3. Acceptance criteria

- [ ] **S1a-AC1** Bead dengan deskripsi 30 baris, 4 item AC, design, dan notes tampil penuh di output `gt prime` (test golden output).
- [ ] **S1a-AC2** Bagian yang melebihi batas terpotong dengan penanda yang menyebut jumlah baris tersisa dan `bd show <id>`.
- [ ] **S1a-AC3** Mode `enforce`: sling bead `feature` tanpa AC ditolak, exit nonzero, pesan menyebut alasan; tidak ada perubahan state bead/hook.
- [ ] **S1a-AC4** Mode `warn`: bead yang sama tetap di-dispatch, peringatan tampil, event `task.readiness` tercatat.
- [ ] **S1a-AC5** Mode `off`: tidak ada cek dan tidak ada event.
- [ ] **S1a-AC6** Scheduler pada mode `enforce` melewati bead tidak siap dengan alasan `not_ready`, dan bead lain tetap di-dispatch.
- [ ] **S1a-AC7** `--force-unready --reason` mencatat actor + alasan sebagai komentar bead dan event `overridden`; tanpa `--reason` ditolak.
- [ ] **S1a-AC8** Mode `enforce`: formula dengan `{{var}}` tak terdefinisi ditolak sebelum cook, pesan menyebut nama variabel. Mode `warn`: sling jalan dengan peringatan. Formula embedded yang ada lulus validasi (tidak ada false positive).
- [ ] **S1a-AC9** Tipe bead selain `task`/`feature`/`bug` (mis. `epic`, `gate`, `chore`) tidak dikenai cek.
- [ ] **S1a-AC10** Error saat membaca bead pada mode `enforce` menolak dispatch (fail-closed).

## 4. Di luar scope

Schema task baru; compiler formula penuh; penilaian kualitas isi AC oleh LLM; `contract_ref` (S3, hanya slot render disiapkan).

## 5. Rollout dan rollback

- Default `warn` di semua rig. Naik ke `enforce` per rig di Tahap 3 setelah satu epic tanpa false reject.
- Rollback: `readiness: off`. Render prime bersifat aditif; batas ukuran bisa diturunkan lewat config.

## 6. Metrik

Bead ditolak/diperingatkan per minggu; rasio request-changes karena salah tafsir requirement dibanding baseline S0.

## Riwayat

- 12 Okt 2026: dibuat dari `09` Rank 1. Ditambahkan: fail-closed saat baca bead gagal (AC10), `--reason` wajib (AC7), pengecualian tipe (AC9).
- 12 Okt 2026: hasil verifikasi kode: definisi item AC = checkbox; override dicatat sebagai komentar; validasi variabel formula mengikuti mode readiness (risiko false positive pada formula extends/compose).
