# S2a — Gate Merge Berbasis Evidence Terikat SHA

Asal: Rank 2 / E05 (`../09` § 4), keputusan D3, D4, Q4. Tahap 2. Impl plan ditulis setelah gate Tahap 1.

## 1. Masalah

Keputusan merge saat ini bergantung pada prosa formula yang dijalankan LLM. Agent bisa menyatakan kegagalan test sebagai "pre-existing" lalu tetap merge, klaim `--pre-verified` tidak dicek, dan pada mode `pr` tanpa GitHub Actions tidak ada check yang ditunggu.

## 2. Perilaku yang diharapkan

- Perintah `gt mq verify <mr> --sha <sha>` memutuskan secara deterministik apakah MR boleh di-merge. Sumber evidence dikonfigurasi per rig:
  - `commit_status:<context>`: membaca status GitHub (mis. `local-ci`) pada SHA yang akan di-merge;
  - `run`: menjalankan gate rig dan mencatat hasilnya.
- Hasil verify ditulis ke bead MR: `{sha, base_sha, source, command, exit, duration, log_ref, verified_at}`, dan event `gate.result`.
- Evidence hanya sah untuk SHA yang sama dengan SHA head PR. Bila `epic/<nama>` sudah maju, PR di-rebase dan gate dijalankan ulang pada SHA baru (gate satu tingkat, ±4 menit).
- Langkah merge di formula refinery wajib memanggil `gt mq verify`; exit nonzero berarti tidak merge dan FIX_NEEDED dikirim dengan referensi log.
- Kegagalan "pre-existing" hanya bisa dilewati lewat label override yang dipasang actor manusia; override tercatat.
- Flaky: satu retry otomatis yang tercatat; gagal lagi berarti gagal.
- `require_review` dan syarat `APPROVED` di formula refinery konsisten dengan config (D3).
- Mode per rig `verify: off | observe | enforce`. `observe` mencatat evidence tanpa memblokir.

## 3. Acceptance criteria

- [ ] **S2a-AC1** MR dengan status `local-ci=failure` atau tanpa status pada SHA yang akan di-merge tidak di-merge; FIX_NEEDED berisi referensi log.
- [ ] **S2a-AC2** Evidence untuk SHA A ditolak saat head PR adalah SHA B.
- [ ] **S2a-AC3** Override "pre-existing" hanya diterima dari label yang dipasang actor manusia, dan tercatat.
- [ ] **S2a-AC4** Setiap bead MR yang merged memiliki record evidence lengkap (query sampel).
- [ ] **S2a-AC5** Mode `observe` mencatat evidence tanpa mengubah keputusan merge.
- [ ] **S2a-AC6** Retry flaky terjadi tepat satu kali dan tercatat.
- [ ] **S2a-AC7** `require_review=false` tidak lagi menuntut `APPROVED`; `true` tetap menuntut.

## 4. Di luar scope

Menghubungkan engine Go refinery penuh; invalidasi evidence berbasis analisis dampak; verifikasi visual (E12); reviewer AI (E11).

## 5. Rollout dan rollback

`observe` selama satu epic, lalu `enforce`. Rollback `verify: off`; formula lama tetap tersedia lewat overlay.

## 6. Gate keluar (bersama S2b)

Setelah `enforce`: 0 merge tanpa evidence dan 0 merge dengan evidence SHA berbeda.

## Riwayat

- 12 Okt 2026: dibuat dari `09` Rank 2. Ditambahkan: mode `observe` sebagai nilai config eksplisit, AC retry flaky dan `require_review`.
