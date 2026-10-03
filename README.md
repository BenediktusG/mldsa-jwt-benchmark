# mldsa-jwt-benchmark

Proyek ini mengimplementasikan layanan HTTP stateless untuk ES256, ES384, ES512, ML-DSA-44, ML-DSA-65, dan ML-DSA-87. Seluruh operasi tanda tangan memakai pustaka standar Go 1.27.1. Jalur HTTP, serialisasi JWS, dan validasi klaim sama untuk keenam algoritma.

## Isi proyek

- `cmd/server`, `internal/`: server, profil JWT, kunci, dan adaptor tanda tangan.
- `cmd/keygen`, `config/`, `keys/`: profil tetap dan enam pasangan kunci lokal. Berkas di `keys/` tidak boleh dipublikasikan.
- `load/scenario.js`: fase k6 15 detik ramp-up, 60 detik pengukuran, dan maksimal 30 detik penyelesaian.
- `runner/schedule.json`, `cmd/experiment`, `internal/experiment`: urutan berurutan 240 pelaksanaan dan orkestrasi Docker/k6 dalam Go.
- `cmd/analyze`, `internal/analysis`: perhitungan ulang CSV mentah, tabel, perbandingan, dan SVG dalam Go.
- `cmd/validate`: validasi dan pencatatan hasil pemeriksaan dalam Go.
- `results/raw/`: CSV k6, log, serta metadata asli per pelaksanaan; `results/processed/`: keluaran yang dapat dibuat ulang.

## Menjalankan server dan validasi

Go 1.27.1 diperlukan. Enam kunci lokal sudah dibuat dengan izin `0600`. Jika membuat proyek dari salinan tanpa kunci, jalankan **sekali**:

```sh
go run ./cmd/keygen -config config/config.json -keys keys
```

Perintah tersebut menolak berkas kunci yang sudah ada agar pasangan kunci penelitian tidak berubah tanpa sengaja. Untuk validasi:

```sh
go test -race ./...
go vet ./...
k6 inspect -e ALG=ES256 -e OPERATION=issue -e TARGET_VU=1 -e RUN_ID=inspect load/scenario.js
```

`go run ./cmd/validate` menjalankan ketiga pemeriksaan tersebut dan menyimpan hasil beserta hash kode dan konfigurasi di `results/validation/`.

Sebelum pengambilan data penelitian, jalankan juga smoke test integrasi Docker/k6. Pengujian ini membangun image server, memakai pasangan kunci sementara, menjalankan masing-masing satu permintaan `issue` dan `verify` melalui k6, lalu memastikan keluaran mentah dapat diproses oleh perintah analisis. Image, kontainer, dan kunci sementara dibersihkan setelah pengujian:

```sh
RUN_DOCKER_K6_SMOKE=1 go test ./internal/experiment -run TestDockerK6AnalysisSmoke -v
```

Untuk menjalankan server langsung:

```sh
go run ./cmd/server -config config/config.json -keys keys
```

`POST /token?alg=ES256` menerima `{"sub":"vu-0001"}` dengan `Content-Type: application/json`. `GET /protected?alg=ES256` menerima `Authorization: Bearer <JWT>`. `GET /healthz` hanya untuk pemeriksaan kesiapan orkestrator. Nilai `sub` yang sah adalah `vu-0001` hingga `vu-1000` dengan panjang byte tetap. Ukuran maksimum header HTTP adalah 16 KiB dan badan `/token` adalah 1024 byte, sama untuk semua algoritma. Layanan menggunakan koneksi HTTP persisten pada loopback melalui port Docker 8080, tanpa TLS atau proxy.

## Menjalankan matriks 240 pelaksanaan

Pasang Docker dan k6 pada mesin inang. Hidupkan Docker daemon. Periksa pemetaan CPU aktual dengan `lscpu -e=CPU,CORE,SOCKET,ONLINE,MAXMHZ` dan pilih keempat CPU logis yang merupakan dua thread dari masing-masing **dua Performance Core fisik yang berbeda**. Contoh berikut mengasumsikan pasangan thread `(0,8)` dan `(1,9)` telah diverifikasi berada pada dua Performance Core:

```sh
go run ./cmd/experiment run --server-cpus 0,8,1,9
```

Penjadwal memvalidasi bahwa alokasi server berisi tepat empat CPU logis, terbagi sebagai dua thread pada masing-masing dua core fisik. Pemeriksaan jenis Performance Core tetap memerlukan verifikasi topologi perangkat. `compose.yaml` membatasi server ke empat thread tersebut, memori 1 GB, dan `GOMAXPROCS=4`. k6 tidak diberi batas afinitas CPU oleh penjadwal sehingga dapat memakai semua CPU yang tersedia bagi proses pada mesin inang. Penjadwal membangun kontainer satu kali, lalu membuat ulang proses untuk setiap pelaksanaan. Ia menghentikan kontainer setelah k6 selesai, tanpa jeda tetap. Jika pelaksanaan gagal atau menghasilkan nol keberhasilan, penjadwal berhenti agar penyebabnya ditinjau.

Untuk melanjutkan bagian tertentu dari jadwal, pakai `--start-index` (mulai dari 0) dan `--limit`. Jangan menimpa CSV mentah. Jika suatu pelaksanaan harus dikeluarkan, buat `runner/exclusions.json` sebagai objek `{"run_id":"alasan terdokumentasi"}`, lalu jalankan ulang indeks yang sama. Analisis menolak dua pelaksanaan sah pada indeks jadwal yang sama. Nilai rendah karena beban sistem bukan alasan pengeluaran.

Jadwal memakai urutan tetap: algoritma, operasi, target VU `1`, `10`, `100`, dan `1000`, lalu putaran `1` sampai `5`. Dengan demikian, setiap skenario dijalankan lima kali secara berurutan sebelum beralih ke skenario berikutnya. Jadwal tidak menggunakan seed atau pengacakan. Untuk membuat berkas jadwal baru, jalankan `go run ./cmd/experiment plan --output <berkas-baru>`, lalu pasang `--schedule <berkas-baru>` saat menjalankan.

## Mengolah data

```sh
go run ./cmd/analyze
```

`per_run.csv` memuat throughput, mean, dan P99 setiap pelaksanaan. `summary.csv` memuat rata-rata, simpangan baku sampel, koefisien variasi, dan jumlah nilai tersedia. `comparison.csv` memuat pasangan ML-DSA terhadap ECDSA dengan selisih relatif terhadap ECDSA. `exclusions.csv` memuat alasan pengeluaran yang dicatat. Enam grafik SVG memisahkan operasi dan metrik, memakai sumbu VU logaritmik dan error bar simpangan baku antar-pengulangan.

Throughput menghitung keberhasilan yang **dimulai dan selesai** dalam jendela 60 detik, dibagi 60. Mean dan P99 memakai keberhasilan yang **dimulai** dalam jendela; respons yang selesai saat fase penyelesaian tetap masuk. P99 menggunakan interpolasi linear tipe 7 pada sampel satu pelaksanaan sebelum hasil antarpelaksanaan diringkas. Permintaan gagal tetap ada dalam CSV mentah, tetapi tidak masuk tiga metrik utama. Sampel k6 menyimpan waktu mulai/selesai sebagai tag `start_ms`/`end_ms` di kolom `extra_tags`, sehingga aturan batas waktu dapat diaudit.

Metadata merekam versi alat, hash sumber dan konfigurasi, ID image Docker server, topologi CPU, profil daya bila tersedia, alokasi sumber daya, ukuran JWT/payload, jumlah token persiapan, batas fase, dan jumlah sukses. Sebelum setiap pelaksanaan, runner memastikan ID image server serta berkas host yang masih digunakan saat runtime (`compose.yaml`, `load/scenario.js`, jadwal, dan kunci yang di-mount) tidak berubah. Kode Go yang sudah dikompilasi ke dalam image tidak di-hash ulang pada setiap pelaksanaan. Identitas JWT dan kunci privat tidak ditulis ke metadata atau CSV. Berkas token persiapan dibuat sementara dengan izin `0600` dan dihapus setelah k6 selesai.

## Batas pelaksanaan saat penyusunan

Pengujian Go dengan race detector, pemeriksaan sintaks k6, dan penerbitan/verifikasi HTTP untuk keenam algoritma telah lulus. Docker daemon tidak tersedia saat proyek disusun; kontainer dan 240 pelaksanaan penelitian belum dijalankan. Data hasil penelitian belum dihasilkan.
