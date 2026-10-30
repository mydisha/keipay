# Design Document: KeiPay

## Overview

KeiPay adalah **payment orchestrator self-hosted** yang memberi banyak aplikasi satu API dan satu format webhook untuk beberapa payment gateway (Xendit, Midtrans, DOKU, dan seterusnya).

Repo ini berbentuk monorepo: `backend/` (Go module `github.com/mydisha/keipay/backend`) dan `frontend/` (admin console React). Path Go di dokumen ini (`cmd/`, `internal/`, `migrations/`) relatif terhadap `backend/`.

Alur intinya:

```
App A ─┐                          ┌─► Xendit   (API key milik merchant)
App B ─┼─► KeiPay (1 credential) ─┼─► Midtrans (API key milik merchant)
App C ─┘        ▲                 └─► DOKU     (API key milik merchant)
                │  webhook dari PG (per connection)
                └──► verifikasi → normalisasi → sign → webhook_url milik App
```

### Prinsip yang tidak boleh dilanggar

1. **KeiPay tidak pernah memegang dana.** Setiap transaksi dibuat dengan kredensial PG milik merchant yang memiliki aplikasi itu, sehingga uang langsung masuk ke akun PG merchant tersebut. KeiPay hanya meneruskan API call dan webhook. Tidak ada saldo bersama dan tidak ada settlement manual.
2. **Aplikasi hanya boleh memakai connection milik merchant yang sama.** Server menegakkan ini, jadi konfigurasi tidak bisa memasukkan uang merchant A ke akun merchant B.
3. **Webhook masuk diverifikasi dengan secret milik connection penerimanya**, sebelum isi body dipercaya.
4. **Fallback tidak boleh menghasilkan dua tagihan aktif** untuk satu payment.

### Goals

- Satu API `POST /v1/payments` untuk banyak PG, dengan `provider` eksplisit atau `auto`.
- Routing `auto`: weighted, priority, atau cheapest, dengan circuit breaker dan fallback saat create.
- Webhook dari semua PG dinormalisasi ke satu format, ditandatangani, dikirim at-least-once, dan bisa di-replay.
- Admin console untuk connection, app, routing, timeline payment, dan webhook.
- Mudah di-self-host: satu binary Go, Postgres, dan Redis.

### Non-goals (v1)

- Direct API (VA, QRIS, e-wallet, kartu) tanpa hosted checkout. v1 hanya mendukung hosted checkout: Xendit Invoice, Midtrans Snap, DOKU Checkout.
- Menyimpan atau memproses data kartu. Dengan hosted checkout, cakupan PCI DSS tetap minimal.
- Refund, payout, atau disbursement.
- Bobot adaptif dari success rate dan strategi `cheapest` (v2).
- Mode SaaS multi-merchant publik (lihat [Catatan hukum](#catatan-hukum)).

## Istilah

| Istilah | Arti |
|---|---|
| **Merchant** | Pemilik bisnis dan pemilik akun PG. Self-host untuk satu bisnis berarti satu merchant default. |
| **Connection** | Satu set kredensial ke satu akun PG milik merchant, misalnya `xendit-prod` atau `midtrans-sandbox`. |
| **Channel** | Metode pembayaran yang dinormalisasi: `QRIS`, `VA_BCA`, `EWALLET_OVO`, `CARD`, dan seterusnya. |
| **App** | Aplikasi klien yang memanggil KeiPay. Punya satu environment (`test` atau `live`), banyak API key (`app_api_keys`, masing-masing dengan secret HMAC sendiri), dan webhook endpoint (`webhook_endpoints`, masing-masing dengan signing secret sendiri). |
| **Payment** | Satu permintaan pembayaran dari app, unik per `(app_id, order_id)`. |
| **Attempt** | Satu percobaan membuat checkout di satu connection. Satu payment bisa punya beberapa attempt karena fallback, tapi paling banyak **satu** yang aktif. |

## Architecture

KeiPay memakai arsitektur ports-and-adapters. Setiap PG punya satu adapter yang mengimplementasikan port `provider.Provider`.

### Posisi komponen

Komponen bertanda *(rencana)* bagian dari routing multi-gateway dan health, yang belum dibangun (lihat [Roadmap](#roadmap)).

```
cmd/server/main.go            manual DI; mendaftarkan adapter ke provider.Registry
internal/
  domain/                     entitas, enum, error, interface repository; hanya standard library
    app.go, api_key.go        App, environment, API key
    connection.go             Merchant, Connection, ConnectionChannel
    channel.go                Channel enum
    payment.go                Payment, PaymentStatus, CanTransitionTo
    event.go, inbound.go      katalog event, envelope, InboundWebhook
    payment_report.go         PaymentReport
    reconciliation.go         run, item, GatewayTransaction
    attempt.go                PaymentAttempt, AttemptStatus (rencana)
    routing.go                RoutingDecision, Strategy (rencana)
  provider/
    provider.go, registry.go  port Provider, Capabilities, Registry, kelas error, schema
    xendit/                   adapter Xendit Invoice
    midtrans/                 adapter Snap (rencana)
    doku/                     adapter Checkout (rencana)
    providertest/             fake Provider untuk test service
  service/
    payment_service.go        create payment; lewat RoutingService + attempt loop (rencana)
    webhook_service.go        Process(InboundEvent) generik
    connection_service.go     CRUD, test credentials, enkripsi kredensial
    webhook_endpoint_service.go, delivery_service.go
    reconciliation_*.go       run rekonsiliasi, matcher, sumber transaksi PG
    routing_service.go        seleksi kandidat dan strategi, logic murni (rencana)
    health_service.go         circuit breaker per (connection, channel) (rencana)
    reconcile_service.go      poll status PG untuk attempt PENDING (rencana; terpisah dari reconciliation_service.go)
  repository/
    postgres/                 satu repo per agregat; attempt_repo (rencana)
    redis/                    sesi, counter rate limit, nonce; health.go (rencana)
  transport/http/
    router.go                 satu-satunya tempat route didaftarkan
    handler/webhook_handler.go  /webhooks/:provider/:connection_id → adapter.VerifyWebhook → ParseWebhook
  worker/
    forwarder.go, sweeper.go  delivery webhook keluar
    reconciliation.go         job run rekonsiliasi
    reconciler.go             task periodik poll status (rencana)
```

### Kenapa port ada di `internal/provider`, bukan di `domain`

Tipe input dan output port ini spesifik untuk integrasi PG (credential schema, raw payload, klasifikasi error), bukan konsep bisnis. Service bergantung pada `provider.Registry`, adapter mengimpor `provider` dan `domain`, dan `domain` tetap tanpa dependensi.

## Alur

Alur di bagian ini adalah desain lengkap dengan routing multi-gateway. Langkah yang memakai `RoutingService`, attempt, dan reconciler status belum dibangun (lihat [Roadmap](#roadmap)). Perilaku create payment dan webhook masuk yang berlaku sekarang ada di [Connections dan provider](#connections-dan-provider) dan [Webhook router](#webhook-router).

### 1. Create payment

```
App ── POST /v1/payments (HMAC) ──► PaymentService.Create
  1. Idempotency: (app_id, order_id) sudah ada?
       sama amount/currency → kembalikan payment yang ada
       beda                 → ErrConflict (409)
  2. RoutingService.Plan(app, input) → daftar kandidat terurut + RoutingDecision
       kosong → ErrNoEligibleConnection (422)
  3. INSERT payment (PENDING) + routing_decision, COMMIT
  4. Untuk setiap kandidat, maksimal fallback.max_attempts kali:
       a. INSERT attempt (CREATING, reference deterministik), COMMIT
       b. adapter.CreateCheckout(creds, input, reference)
       c. hasilnya:
            OK                → attempt CREATED; payment.connection/provider/checkout_url diisi; selesai
            ErrRejected (4xx) → attempt FAILED; BERHENTI, kembalikan 400/422 ke app (tidak fallback)
            ErrRetryable      → attempt FAILED; health.RecordFailure; lanjut ke kandidat berikutnya
            ErrUnknownOutcome → resolveUnknown(attempt) (lihat di bawah)
  5. Semua kandidat habis → payment FAILED, ErrProviderUnavailable (503, retryable)
```

`resolveUnknown` (timeout atau koneksi putus setelah request terkirim):

```
adapter.FindByReference(creds, reference)
  ditemukan     → ADOPSI: attempt CREATED dengan data yang ditemukan; selesai (tidak membuat tagihan kedua)
  tidak ada     → attempt FAILED; aman untuk fallback
  lookup gagal  → attempt UNKNOWN; BERHENTI, kembalikan 503 ke app;
                  reconciler akan menyelesaikannya (adopsi atau batalkan)
```

Langkah 3 dan 4a di-commit **sebelum** memanggil PG, supaya crash di tengah jalan selalu meninggalkan jejak yang bisa direkonsiliasi. Pola ini menggantikan "invoice yatim di-expire" yang dipakai `PaymentService.Create` saat ini.

### 2. Webhook masuk dari PG

```
PG ── POST /webhooks/{provider}/{connection_public_id} ──► WebhookHandler
  1. Resolve connection by public_id; provider di path harus sama → kalau tidak, 404
  2. adapter.VerifyWebhook(creds, headers, rawBody) → gagal: 401 + security event
  3. adapter.ParseWebhook(rawBody) → InboundEvent{Reference, ProviderPaymentID, EventKey,
                                                    Status (sudah dinormalisasi), Amount, Currency, Channel, Fee, PaidAt}
  4. WebhookService.Process(connection, event, raw), dalam satu transaksi:
       - CreateInboundIfNew(connection_id, event_key, provider_status) → duplikat: ack 200, selesai
       - attempt = GetAttemptByReferenceForUpdate(connection_id, reference)
       - lock payment; cek amount/currency (mismatch → catat, tidak di-apply, security event)
       - attempt bukan attempt aktif dan status sukses → tandai DUPLICATE_PAYMENT, event payment.duplicate
       - CanTransitionTo → update status, ledger/report entry, delivery row (PENDING)
  5. COMMIT, lalu enqueue forward; ack 200 ke PG
```

Routing ke app dilakukan **lewat lookup attempt**, bukan parsing `external_id`, sehingga slug app tidak dikirim ke PG.

### 3. Forward ke app

Satu `events` per perubahan status, fan-out menjadi satu `webhook_deliveries` per endpoint app yang subscription-nya cocok, retry per endpoint yang dijadwalkan worker sendiri, `Sweeper` untuk delivery yang terlambat, dan replay dari admin. Detailnya di [Webhook router](#webhook-router).

### 4. Rekonsiliasi status

Berbeda dari [Rekonsiliasi](#rekonsiliasi) laporan: rekonsiliasi status memajukan payment yang callback-nya hilang, sedangkan rekonsiliasi laporan membandingkan catatan KeiPay dengan catatan PG.

**Checkout kedaluwarsa (sudah berjalan).** PG bisa tidak mengirim callback, misalnya notifikasi invoice expired di dashboard PG mati atau callback tidak sampai, sehingga payment tertahan `PENDING` selamanya. `worker.CheckoutChecker` berjalan tiap menit dan memanggil `CheckoutCheckService.CheckOverdue`:

- Mengambil maksimal 50 payment `PENDING` yang punya connection dan ID pembayaran PG, dengan `expires_at` lebih dari 2 menit lalu (PG diberi waktu mengirim callback-nya sendiri) dan kurang dari 72 jam lalu, yang belum pernah dicek atau terakhir dicek 10 menit lalu atau lebih (`payments.status_checked_at`, indeks parsial `idx_payments_pending_expiry`).
- Untuk connection yang provider-nya mendeklarasikan capability `read_checkout`, membaca checkout lewat interface opsional `provider.CheckoutReader` (`GetCheckout`; Xendit: `GET /v2/invoices/{id}` dengan HTTP biasa, dibaca dengan parser callback yang sama). Hasilnya `InboundEvent{Source: "poll"}`.
- Status yang masih pending tidak menulis apa pun. Status lain dimasukkan lewat `WebhookService.Process` yang sama dengan callback, sehingga klaim dedupe `(connection_id, event_key, provider_status)`, state machine, payment report, event, dan delivery-nya sama persis; baris `inbound_webhooks` bersumber `poll` (`signature_valid` true, `raw_body` jawaban PG). Callback yang datang belakangan tercatat `duplicate`.
- Setiap payment yang dicoba ditandai sudah dicek apa pun hasilnya, jadi PG yang gagal hanya ditanya lagi setelah 10 menit. Jawaban untuk reference lain tidak diterapkan.

**Attempt (rencana, bersama routing multi-gateway).** `worker.Reconciler` berjalan periodik (default 5 menit):

- Attempt `CREATED` dengan payment `PENDING` yang lebih tua dari `RECONCILE_MIN_AGE` (default 15 menit) dan belum kedaluwarsa → `adapter.GetStatus`. Kalau statusnya maju, dimasukkan lewat `WebhookService.Process` yang sama sebagai `InboundEvent{Source: "poll"}`, sehingga idempotency dan state machine-nya sama persis dengan webhook.
- Attempt `UNKNOWN` → `FindByReference`. Kalau ditemukan dan payment belum punya attempt aktif, attempt itu diadopsi. Kalau sudah ada attempt aktif lain, attempt yang ditemukan dibatalkan lewat `CancelCheckout`.
- Batas rate per connection supaya tidak memicu rate limit PG.

## Components and Interfaces

### Port `provider.Provider`

```go
package provider

type Name string // "xendit", "midtrans", "doku"

type Credentials map[string]string // hasil dekripsi; key sesuai CredentialSchema
type Settings map[string]any       // setting tervalidasi, default sudah diisi

// Account diterima setiap call ke PG.
type Account struct {
	Environment domain.Environment
	Credentials Credentials
	Settings    Settings
}

type Capabilities struct {
	Channels            []domain.Channel
	Currencies          []string
	CanRestrictChannels bool // hosted checkout bisa dibatasi ke channel tertentu
	CanCancel           bool
	ReportsFee          bool // callback membawa fee aktual
	ListTransactions    bool // adapter mengimplementasikan TransactionLister
}

type CheckoutInput struct {
	Reference          string           // reference gateway payment (payments.external_id)
	Amount             int64
	Currency           string
	Description        string
	PayerEmail         string
	Channels           []domain.Channel // kosong = tidak dibatasi
	Items              []domain.OrderItem
	Customer           *domain.Customer
	SuccessRedirectURL string
	FailureRedirectURL string
	ExpiresIn          time.Duration    // 0 = setting connection, lalu default PG
}

type CheckoutOutput struct {
	ProviderPaymentID string
	CheckoutURL       string
	Status            domain.PaymentStatus
	ExpiresAt         *time.Time
	Raw               []byte
}

type InboundEvent struct {
	EventKey          string // kunci dedupe bersama ProviderStatus
	Reference         string
	ProviderPaymentID string
	ProviderStatus    string               // status asli PG, untuk audit
	Status            domain.PaymentStatus // kosong kalau status PG tidak dikenal
	Amount            int64
	Currency          string
	PaidAmount        int64
	Fee               *int64 // potongan PG dari merchant; nil = tidak diketahui
	PaidAt            *time.Time
	SettledAt         *time.Time
	Source            string // "webhook" | "poll"
}

type Provider interface {
	Descriptor() Descriptor // nama tampilan, deskripsi, URL dokumentasi, petunjuk URL webhook
	CredentialSchema() Schema
	SettingsSchema() Schema
	Capabilities() Capabilities
	ValidateAccount(env domain.Environment, c Credentials) []domain.FieldError

	TestCredentials(ctx context.Context, a Account) error
	CreateCheckout(ctx context.Context, a Account, in CheckoutInput) (*CheckoutOutput, error)
	CancelCheckout(ctx context.Context, a Account, providerPaymentID string) error

	VerifyWebhook(a Account, h http.Header, rawBody []byte) error
	ParseWebhook(rawBody []byte) (*InboundEvent, error)
}

// Opsional, untuk rekonsiliasi; wajib kalau Capabilities.ListTransactions.
type TransactionLister interface {
	ListTransactions(ctx context.Context, a Account, q TransactionQuery) (*TransactionPage, error)
}
```

`VerifyWebhook` cukup membaca header; port ini tidak mengimpor Gin. Detail schema, `Descriptor`, dan `ValidateAccount` ada di [Connections dan provider](#connections-dan-provider).

Routing multi-gateway (rencana) menambah dua method ke port, dipakai attempt loop dan reconciler status, beserta `Channel` di `InboundEvent`:

```go
type StatusOutput struct {
	ProviderPaymentID string
	Status            domain.PaymentStatus
	Channel           domain.Channel
	Amount            int64
	Currency          string
	Fee               *int64
	PaidAt            *time.Time
	Raw               []byte
}

FindByReference(ctx context.Context, a Account, reference string) (*StatusOutput, error) // ErrNotFound kalau tidak ada
GetStatus(ctx context.Context, a Account, providerPaymentID string) (*StatusOutput, error)
```

Adapter ada di repo yang sama, jadi menambah method ke interface tidak memutus siapa pun.

### Klasifikasi error

Adapter **wajib** membungkus setiap error ke salah satu kelas berikut (`errors.Is`). Logika fallback bergantung sepenuhnya pada kelas ini.

| Kelas | Kapan | Fallback? |
|---|---|---|
| `ErrRejected` | 4xx validasi, kredensial salah, amount di luar batas | Tidak |
| `ErrRetryable` | Koneksi ditolak, DNS gagal, 5xx/429 **sebelum** PG menerima request, circuit open | Ya |
| `ErrUnknownOutcome` | Timeout atau koneksi putus **setelah** request terkirim | Hanya setelah `FindByReference` memastikan transaksi tidak ada |
| `ErrNotFound` | `FindByReference` atau `GetStatus` tidak menemukan data | – |

Kalau ragu antara `ErrRetryable` dan `ErrUnknownOutcome`, adapter harus memilih `ErrUnknownOutcome`.

### Registry

```go
func NewRegistry(ps ...Provider) (*Registry, error) // menolak nama ganda dan ListTransactions tanpa TransactionLister
func (r *Registry) Get(n Name) (Provider, error)
func (r *Registry) Has(n Name) bool
func (r *Registry) All() []Provider // untuk UI: daftar provider + schema
```

Dibuat di `cmd/server/main.go` (`provider.NewRegistry(xendit.New())`). Menambah PG baru berarti menulis satu paket adapter dan satu argumen registrasi.

### Catatan per adapter

Adapter Xendit sudah dibangun. Kolom Midtrans dan DOKU adalah rencana dan **harus diverifikasi ulang terhadap dokumentasi resmi** saat adapternya ditulis.

| | Xendit (Invoice) | Midtrans (Snap) | DOKU (Checkout) |
|---|---|---|---|
| Reference di PG | `external_id` | `order_id` (unik per merchant, maks 50 karakter) | `order.invoice_number` |
| Batasi metode | `payment_methods` | `enabled_payments` | `payment.payment_method_types` |
| Verifikasi webhook | header `x-callback-token` == token connection (constant-time) | `signature_key` = SHA512(`order_id`+`status_code`+`gross_amount`+server key) | header `Signature`: HMAC-SHA256 atas Client-Id, Request-Id, Timestamp, Target, Digest |
| Lookup by reference | `GET /v2/invoices?external_id=` | `GET /v2/{order_id}/status` | API status by invoice number |
| Batalkan | expire invoice | `POST /v2/{order_id}/cancel` atau expire | verifikasi saat implementasi |
| Kredensial | `secret_key`, `callback_token` | `server_key`, `client_key` (opsional) | `client_id`, `secret_key` |

Adapter Xendit ada di `internal/provider/xendit`; pemetaan status Xendit ada di `ParseWebhook` adapter itu, bukan di `webhook_service.go`.

### Normalisasi status

State machine `domain.PaymentStatus` memakai `CanTransitionTo` (PENDING → PAID → SETTLED; sukses yang terlambat setelah EXPIRED/FAILED tetap di-apply). Setiap adapter memetakan status aslinya:

| Normal | Xendit | Midtrans `transaction_status` | DOKU |
|---|---|---|---|
| `PENDING` | `PENDING` | `pending`; `capture` + `fraud_status=challenge` | pending |
| `PAID` | `PAID` | `settlement`; `capture` + `fraud_status=accept` | success |
| `SETTLED` | `SETTLED` | tidak ada (Midtrans tidak mengirim event dana cair) | tidak ada |
| `EXPIRED` | `EXPIRED` | `expire` | expired |
| `FAILED` | `FAILED`, `STOPPED` | `deny`, `cancel`, `failure` | failed |

Status yang tidak dikenal → `ParseWebhook` mengembalikan `Status` kosong, inbound dicatat dengan `result = unmapped`, dan PG tetap di-ack. Webhook tidak boleh di-apply diam-diam, dan status yang tidak dikenal tidak pernah dipetakan ke nilai default.

`SETTLED` bersifat opsional. App tidak boleh bergantung pada event ini karena hanya sebagian PG yang mengirimnya.

## Routing

Routing multi-gateway dan health belum dibangun (lihat [Roadmap](#roadmap)). Saat ini setiap payment memakai default connection app-nya (lihat [Connections dan provider](#connections-dan-provider)).

### Konfigurasi per app

```yaml
routing:
  strategy: weighted        # weighted | priority | cheapest (v2)
  allow_explicit_fallback: false   # provider eksplisit boleh fallback?
  fallback:
    enabled: true
    max_attempts: 2
  connections:              # urutan list = prioritas
    - connection: xendit-prod
      weight: 60
      enabled: true
    - connection: midtrans-prod
      weight: 30
    - connection: doku-prod
      weight: 10
  adaptive:                 # v2
    enabled: false
    min_samples: 200
    window: 7d
    exploration: 0.05
```

Disimpan di tabel `app_connections` (weight, priority, enabled) dan `apps.routing_config` (strategy, fallback). Tidak dalam bentuk YAML. Contoh di atas hanya untuk ilustrasi.

### Algoritma `RoutingService.Plan`

```
input: app, request{provider, connection, methods, amount, currency}

1. kandidat = app_connections(app).enabled ∩ connections(merchant = app.merchant).active
2. explicit?
     request.connection → kandidat = [itu]          (tidak ada / bukan milik merchant → 422)
     request.provider   → kandidat = filter provider == request.provider
3. filter kelayakan (alasan setiap penolakan dicatat):
     - currency didukung
     - methods kosong ATAU ada irisan dengan channel aktif di connection
     - amount dalam [min, max] channel yang relevan
     - tidak dalam jendela maintenance
     - circuit breaker connection tidak OPEN
4. hapus channel yang sedang down/maintenance dari `methods` per kandidat
   (supaya hosted checkout tidak menampilkan VA BCA saat VA BCA down)
5. urutkan:
     priority → urutan list
     weighted → weighted random TANPA pengembalian (draw sampai habis)
     cheapest → estimasi fee naik, seri → urutan list            (v2)
6. explicit && !allow_explicit_fallback → potong jadi 1 kandidat
7. potong jadi fallback.max_attempts (atau 1 kalau fallback dimatikan)
8. kembalikan kandidat + RoutingDecision{strategy, considered[], rejected[{conn, reason}], scores}
```

`RoutingService` adalah logic murni: input berupa snapshot konfigurasi dan health, output berupa daftar. Sumber angka acaknya di-inject, jadi bisa di-test secara deterministik.

### Health dan circuit breaker

- **Granularitas:** `(connection_id, channel)`. Error saat create (channel belum diketahui) dicatat di level connection dengan `channel = "*"`.
- **Penyimpanan:** Redis, bucket per menit `health:{conn}:{channel}:{yyyymmddhhmm}` → `HINCRBY ok|err|timeout`, `latency_ms_sum`, TTL 1 jam.
- **Breaker:**
  - `CLOSED` → `OPEN` kalau dalam 5 menit terakhir `requests ≥ 20` dan `error_rate ≥ 50%`.
  - `OPEN` selama `BREAKER_OPEN_FOR` (default 2 menit) → `HALF_OPEN`.
  - `HALF_OPEN`: maksimal 1 request percobaan per 10 detik. Kalau sukses → `CLOSED`, kalau gagal → `OPEN` lagi.
  - Semua angka bisa dikonfigurasi lewat env.
- **Override manual:** `connection_channels.maintenance_until` dan toggle per connection dari admin. Jendela terjadwal disimpan sebagai baris dengan waktu mulai dan selesai.
- **Dashboard 24 jam** dihitung dari tabel `payment_attempts` (yang sudah di-index `created_at`), bukan dari Redis.
- **Redis tidak tersedia:** health dianggap `CLOSED` (fail-open), dicatat sebagai warning. Routing tetap berjalan dengan bobot statis.

### Bobot adaptif (v2)

`efektif = weight_dasar × faktor_health × faktor_conversion`, dengan:
- `faktor_health` dari success rate API create (sinyal kuat),
- `faktor_conversion` dari PAID/created per channel, hanya kalau sampel ≥ `min_samples`, dibatasi ke rentang [0,5; 1,5],
- `exploration` (default 5%): sebagian traffic memakai bobot dasar, supaya statistik connection yang sempat jelek tetap diperbarui.

## Data Models

Tabel yang sudah dibangun dijelaskan per area di [Konvensi](#konvensi): `users`, `audit_logs`, `apps`, `app_api_keys`, `merchants`, `connections`, `connection_channels`, `webhook_endpoints`, `events`, `webhook_deliveries`, `inbound_webhooks`, `payment_reports`, `reconciliation_runs`, dan `reconciliation_items`. Bagian ini berisi skema yang direncanakan untuk routing multi-gateway. Semua perubahan memakai migrasi expand/contract di `migrations/`, dan setiap migrasi punya `down`.

```sql
ALTER TABLE apps ADD COLUMN routing_config JSONB NOT NULL DEFAULT '{}';

CREATE TABLE app_connections (
  app_id         BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  connection_id  BIGINT NOT NULL REFERENCES connections(id),
  priority       INT NOT NULL,
  weight         INT NOT NULL DEFAULT 1 CHECK (weight >= 0),
  enabled        BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (app_id, connection_id)
);
-- invariant merchant sama dicek di service (ConnectionService/AppService), bukan FK

-- ditambahkan secara additive bersama fitur yang memakainya
ALTER TABLE connection_channels ADD COLUMN min_amount BIGINT;
ALTER TABLE connection_channels ADD COLUMN max_amount BIGINT;
ALTER TABLE connection_channels ADD COLUMN fee_flat BIGINT NOT NULL DEFAULT 0;   -- untuk strategi cheapest (v2)
ALTER TABLE connection_channels ADD COLUMN fee_bps  INT    NOT NULL DEFAULT 0;   -- basis point
ALTER TABLE connection_channels ADD COLUMN maintenance_from  TIMESTAMPTZ;
ALTER TABLE connection_channels ADD COLUMN maintenance_until TIMESTAMPTZ;

ALTER TABLE payments ADD COLUMN active_attempt_id BIGINT;
ALTER TABLE payments ADD COLUMN channel TEXT;                -- channel yang dipakai pembayar
ALTER TABLE payments ADD COLUMN methods TEXT[];              -- yang diminta app
ALTER TABLE payments ADD COLUMN routing_decision JSONB;
ALTER TABLE payments ADD COLUMN duplicate_paid BOOLEAN NOT NULL DEFAULT false;
-- invoice_url menjadi checkout_url lewat expand/contract
-- xendit_invoice_id dipindah ke payment_attempts.provider_payment_id lalu di-drop

CREATE TABLE payment_attempts (
  id                   BIGSERIAL PRIMARY KEY,
  payment_id           BIGINT NOT NULL REFERENCES payments(id),
  attempt_no           INT NOT NULL,
  connection_id        BIGINT NOT NULL REFERENCES connections(id),
  provider             TEXT NOT NULL,
  reference            TEXT NOT NULL,             -- kp_<payment_public_id>_<attempt_no>
  provider_payment_id  TEXT,
  status               TEXT NOT NULL,             -- CREATING|CREATED|FAILED|UNKNOWN|ABANDONED|CANCELLED
  error_class          TEXT,                      -- rejected|retryable|unknown_outcome
  error_message        TEXT,
  latency_ms           INT,
  checkout_url         TEXT,
  raw_response         JSONB,
  created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (payment_id, attempt_no),
  UNIQUE (connection_id, reference)
);
CREATE INDEX payment_attempts_reconcile_idx
  ON payment_attempts (status, created_at) WHERE status IN ('CREATED','UNKNOWN');
CREATE INDEX payment_attempts_conn_time_idx ON payment_attempts (connection_id, created_at);
```

Catatan terhadap skema yang ada:

- `payments.external_id` (`NOT NULL UNIQUE`) saat ini menyimpan reference gateway: `kp_<payment_public_id>`, atau `{slug}:{order_id}` untuk payment lama. Dengan attempt, lookup callback memakai `payment_attempts.reference`; `external_id` dibiarkan untuk payment yang sudah ada, dan `NOT NULL`-nya dilepas di versi mayor berikutnya.
- `payments.xendit_invoice_id` dan index `idx_payments_xendit_invoice_id` di-backfill ke `payment_attempts` (satu attempt `CREATED` per payment lama, `reference = external_id`), lalu di-drop setelah adapter Xendit hanya membaca dari attempt.
- Index lama yang memakai `tenant_id` dibiarkan sampai migrasi contract; index padanannya di `app_id` sudah ada.

### Penyimpanan kredensial

- **Envelope encryption.** Setiap connection punya data key acak; kredensial dienkripsi AES-256-GCM dengan data key itu, dan data key dienkripsi dengan master key dari env `ENCRYPTION_KEY` (master key yang sama untuk secret API key dan webhook). `key_version` memungkinkan rotasi master key tanpa mengubah format. Detailnya di [Connections dan provider](#connections-dan-provider).
- Kredensial **tidak pernah** dikembalikan oleh API admin setelah disimpan. Yang dikembalikan hanya 4 karakter terakhir per field secret.
- Log dan audit memakai `CredentialSchema` untuk menyamarkan field rahasia.
- Pakai API key PG dengan permission minimal (misalnya Xendit restricted key untuk invoice, ditambah baca transaksi kalau connection itu direkonsiliasi).

## API Surface

Kontrak HTTP yang berlaku ada di `api/openapi.yaml`. Bagian `/v1` di bawah ini adalah perluasan yang direncanakan untuk routing multi-gateway; semuanya additive.

### `POST /v1/payments`

Header dan HMAC tidak berubah (`X-Api-Key`, `X-Timestamp`, `X-Signature`).

```json
{
  "order_id": "ord-123",
  "amount": 150000,
  "currency": "IDR",
  "provider": "auto",
  "connection": null,
  "methods": ["QRIS", "VA_BCA"],
  "description": "Paket Pro",
  "items": [...],
  "customer": {...},
  "expires_in": 86400
}
```

- `provider`: `auto` (default) | `xendit` | `midtrans` | `doku`.
- `connection`: nama connection; lebih spesifik dari `provider`, dan keduanya tidak boleh diisi bersamaan.
- `methods`: opsional. Kosong berarti semua channel aktif di connection terpilih.

Respons `201`:

```json
{
  "payment_id": "pay_01J...",
  "order_id": "ord-123",
  "status": "PENDING",
  "amount": 150000,
  "currency": "IDR",
  "provider": "midtrans",
  "connection": "midtrans-prod",
  "checkout_url": "https://app.midtrans.com/snap/v4/redirection/...",
  "invoice_url": "https://...",
  "expires_at": "2026-10-29T10:02:04Z"
}
```

`invoice_url` adalah alias `checkout_url` untuk integrasi yang sudah ada. Alias ini dihapus di versi mayor berikutnya.

| Error | HTTP | `code` |
|---|---|---|
| Validasi input | 400 | `invalid_input` |
| Connection tidak ada atau bukan milik merchant | 422 | `unknown_connection` |
| Tidak ada kandidat yang layak | 422 | `no_eligible_connection` (+ `reasons[]`) |
| PG menolak (4xx) | 422 | `provider_rejected` (+ pesan PG yang sudah disanitasi) |
| Semua kandidat gagal, atau hasilnya belum pasti | 503 | `provider_unavailable` (+ `Retry-After`) |
| order_id dipakai ulang dengan isi berbeda | 409 | `conflict` |

Setelah 503 `provider_unavailable`, app boleh mengulang request dengan `order_id` yang sama. Idempotency menjamin request ulang itu memakai payment yang sama dan tidak membuat tagihan dobel, termasuk saat attempt sebelumnya berstatus `UNKNOWN` dan masih menunggu reconciler.

### `GET /v1/payments/:id`

Respons yang ada ditambah `provider`, `connection`, dan `channel`.

### Webhook keluar

Bentuk webhook keluar (envelope `{id, type, api_version, created_at, data}`, endpoint per app, header `X-KeiPay-*`) dijelaskan di [Webhook router](#webhook-router).

- Event: `payment.paid`, `payment.settled`, `payment.expired`, `payment.failed`, `payment.duplicate`.
- Signature mencakup timestamp, sehingga app bisa menolak replay lama (toleransi yang disarankan 5 menit). Header `X-Kei-Signature` (HMAC body saja) hanya dikirim ke endpoint yang mengaktifkan `legacy_signature`.
- At-least-once. App melakukan dedupe dengan id event (`id` di envelope `v2`, `event_id` di body `v1`).
- `channel` dan `provider_fee` belum ada di `data`; keduanya ditambahkan secara additive saat datanya tersedia.

### Admin API (ringkas)

Kontrak lengkapnya ada di `api/openapi.yaml`.

```
GET             /admin/setup
POST            /admin/setup | /login | /logout
GET             /admin/me
POST            /admin/me/password
GET/POST        /admin/users
GET/PATCH       /admin/users/:id
POST            /admin/users/:id/disable | /enable | /reset-password
GET             /admin/audit-logs

GET/POST        /admin/apps
GET/PATCH       /admin/apps/:id
POST            /admin/apps/:id/disable | /enable
GET/POST        /admin/apps/:id/keys
POST            /admin/apps/:id/keys/:key_id/rotate | /revoke
GET/POST        /admin/apps/:id/webhook-endpoints
GET/PATCH/DELETE /admin/webhook-endpoints/:id
POST            /admin/webhook-endpoints/:id/rotate-secret | /disable | /enable | /test
GET             /admin/event-types

GET             /admin/providers                 (daftar adapter + schema untuk form UI)
GET/POST        /admin/connections
GET/PATCH       /admin/connections/:id
PATCH           /admin/connections/:id/credentials
PUT             /admin/connections/:id/channels
POST            /admin/connections/:id/test | /disable | /enable

GET             /admin/payments
GET             /admin/payment-reports | /summary | /export
GET/POST        /admin/reconciliation-runs
GET             /admin/reconciliation-runs/:id | /items | /items/export
POST            /admin/reconciliation-runs/:id/rerun
GET             /admin/reconciliation-items/:id
POST            /admin/reconciliation-items/:id/resolve

GET             /admin/webhook-deliveries | /admin/webhook-deliveries/:id
POST            /admin/webhook-deliveries/:id/replay
GET             /admin/inbound-webhooks | /admin/inbound-webhooks/:id
```

Rencana untuk routing multi-gateway dan health:

```
POST            /admin/connections/:id/maintenance
GET/PUT         /admin/apps/:id/routing
POST            /admin/apps/:id/routing/simulate (dry-run Plan, tidak memanggil PG)
GET             /admin/payments/:id/timeline     (attempts + inbound + deliveries, terurut waktu)
GET             /admin/health                    (state breaker per connection/channel)
```

Semua list memakai cursor pagination (lihat [Konvensi](#konvensi)). Setiap endpoint admin yang mengubah state dicatat di `audit_logs`.

## Admin Console (frontend/)

Stack: React 19 + Vite + TypeScript sebagai SPA yang disajikan backend Go; Tailwind v4 dengan shadcn/ui (style new-york, Radix, CSS variables untuk light dan dark); ikon lucide; font IBM Plex Sans dan IBM Plex Mono; react-router 7; TanStack Query untuk server state dan TanStack Table untuk tabel; react-hook-form + zod; sonner; cmdk; openapi-typescript + openapi-fetch untuk client dari kontrak. Test memakai Vitest + Testing Library dan Playwright (headless). Library UI atau state management lain hanya ditambah dengan alasan yang dicatat di dokumen ini.

Token, komponen dasar, pola tabel, pola halaman detail, aturan state loading/empty/error, mobile, dark mode, dan format Rp/tanggal/ID ada di `DESIGN.md` di root repo. Badge health (sehat, menurun, down, maintenance, circuit open) ditambahkan ke sana bersama fitur health.

Struktur kode: `src/app` (router, providers, layout), `src/features/<fitur>/{api,components,pages,schemas}`, `src/shared/{ui,lib,api}`. Tipe API di `src/shared/api/schema.d.ts` di-generate dari `api/openapi.yaml` (`make gen`) dan tidak diedit tangan.

Navigasi (sidebar yang bisa diciutkan, top bar dengan switch Test/Live dan banner amber saat Test, command palette ⌘K, toggle tema):

| Menu | Isi | Belum dibangun |
|---|---|---|
| Dashboard | Volume per provider, success rate create, status health, delivery gagal, item rekonsiliasi belum resolved | Seluruhnya; saat ini empty state |
| Apps | List cursor → detail dengan tab Overview, API keys, Webhooks, Routing | Tab Routing (empty state) |
| Connections | List, wizard tambah connection, detail (setting, kredensial, channel, test, aktif/nonaktif), health per channel, toggle maintenance | Health dan maintenance |
| Webhooks | Tab Outbound (delivery ke app, replay) dan Inbound (dari PG, raw body + hasil verifikasi), dengan cursor, filter, dan detail berisi log attempt; endpoint diatur di tab Webhooks detail app | – |
| Payments | List + **timeline** per payment | Timeline |
| Reconciliation | Ringkasan per periode × app × connection, daftar report, run rekonsiliasi per connection, resolve item, export CSV | – |
| Settings | Akun (ganti password), user dan role, audit log | – |

Layar kunci:

1. **Wizard tambah connection:** pilih provider → form dibentuk dari `CredentialSchema` → *Test connection* → tampilkan URL webhook connection itu beserta tombol copy dan petunjuk di menu mana URL dipasang di dashboard PG → aktifkan channel. Fee per channel ditambahkan bersama routing.
2. **Routing per app** (rencana): pilihan strategi, daftar connection yang bisa di-drag (urutan, bobot, health 24 jam, status per channel), pengaturan fallback, tombol *Simulasikan routing* (memanggil `/routing/simulate`).
3. **Timeline payment** (rencana): request app → keputusan routing (kandidat, skor, alasan penolakan) → setiap attempt → inbound webhook (raw, verifikasi) → normalisasi → setiap percobaan delivery.
4. **Onboarding saat pertama dijalankan** (rencana): tambah connection sandbox → buat app → copy kredensial → kirim payment uji → lihat webhook sampai.

Status selalu ditampilkan sebagai teks label, tidak hanya warna. Setiap layar dicek di lebar mobile.

## Konvensi

### Kontrak HTTP

- `api/openapi.yaml` (OpenAPI 3.1) adalah sumber kebenaran kontrak. Setiap perubahan endpoint mengubah spec di commit yang sama. Spec di-lint dengan Redocly (`make lint-api`, aturan di `api/redocly.yaml`).
- Tipe TypeScript di-generate dengan openapi-typescript ke `frontend/src/shared/api/schema.d.ts` (`make gen`) dan dipakai lewat openapi-fetch. Hasil generate di-commit; gerbang `make ci` gagal kalau hasilnya tidak sinkron dengan spec.
- Webhook keluar ke app didokumentasikan di bagian `webhooks` spec.
- **Batas dan header.** Body request dibatasi per grup lewat `middleware.MaxBody` (64 KiB di `/v1`, 1 MiB di `/admin`; callback PG 1 MiB di handler-nya), dijawab `413 request_too_large`; body JSON yang diikuti data lain selain whitespace ditolak `400`. Server memasang `ReadTimeout` 30s, `WriteTimeout` 60s, `IdleTimeout` 120s, dan `MaxHeaderBytes` 64 KiB; export CSV memajukan write deadline-nya sendiri di setiap flush (`csvWriteGrace`, 60s) supaya unduhan panjang tetap utuh sementara klien yang berhenti membaca tetap diputus. Setiap respons membawa `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy: no-referrer`, `Permissions-Policy`, `Cross-Origin-Opener-Policy: same-origin`, `Cache-Control: no-store`, dan CSP API `default-src 'none'; frame-ancestors 'none'`; handler konsol menggantinya dengan CSP konsol (`script-src 'self'`, `style-src 'self' 'unsafe-inline'` karena Radix dan sonner menyisipkan `<style>` saat runtime, `font-src`/`img-src 'self' data:`). `Strict-Transport-Security` dikirim bila `HSTS_ENABLED` (`auto` = mengikuti `COOKIE_SECURE`). Tidak ada CORS: konsol disajikan same-origin. Redirect trailing-slash Gin dimatikan (`RedirectTrailingSlash`, `RedirectFixedPath`): redirect itu dijawab sebelum middleware mana pun, jadi tanpa header keamanan dan request id, dan 301 bisa di-cache; path seperti itu menjadi 404 JSON biasa. Gin berjalan dalam release mode kecuali `APP_ENV=development`. `X-Request-ID` dari klien hanya dipakai bila cocok `^[A-Za-z0-9._-]{1,128}$`.

### ID publik

- Format `<prefix>_<ULID>`, misalnya `pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C`: ULID 26 karakter Crockford base32 huruf besar, sehingga ID terurut menurut waktu pembuatan. Parse menerima huruf kecil dan menormalkannya.
- Prefix: `app_`, `pay_`, `con_`, `whe_`, `evt_`, `dlv_` (delivery webhook), `inb_` (webhook masuk), `usr_`, `key_`, `rec_` (run rekonsiliasi), `rci_` (item rekonsiliasi), `aud_`. Paket `internal/pkg/id` membuat, mem-parse, dan memvalidasi ID; prefix yang salah ditolak.
- Kolom `public_id TEXT NOT NULL UNIQUE` disimpan di samping `id BIGSERIAL` internal. ID bigint tidak keluar lewat API.
- Semua respons admin memakai ID publik, termasuk referensi app dan payment di baris report dan delivery. Baris payment report diidentifikasi dengan `pay_` milik payment-nya. Satu-satunya bigint yang keluar adalah `payment_id` di `/v1` dan di payload webhook v1, demi kompatibilitas integrasi yang sudah ada; `GET /v1/payments/{id}` menerima `pay_…` maupun integer.

### Error

- Semua error, di semua surface, berbentuk `{"error":{"code":"...","message":"...","details":{}}}`. `details` selalu objek.
- `code` stabil dan didefinisikan sebagai konstanta di `internal/domain`; `response.Error` memetakan error domain ke status HTTP dan code. Daftar lengkap ada di `ErrorCode` di spec dan dijaga sinkron oleh test backend. Code baru boleh ditambah; arti code lama tidak berubah.
- Error 5xx selalu memakai message generik; detail internal hanya masuk log.
- Kegagalan validasi field memakai `invalid_request` dengan `details.fields = [{field, rule}]` (nama field JSON, dot path untuk field bersarang). Pelanggaran aturan bisnis memakai `validation_failed`.
- Elemen array di body ikut divalidasi per elemen. Contoh: item `POST /v1/payments` yang tidak valid menghasilkan `400 invalid_request` dengan `details.fields` berisi `{field: "items.0.quantity", ...}`, bukan `validation_failed`. Status tetap 400.
- Path id yang bukan integer → `400 invalid_request` (`field: "id"`, `rule: "int"`). Id integer di bawah 1 tidak divalidasi terpisah: tidak cocok dengan baris mana pun, jadi hasilnya `404 not_found` seperti id yang tidak ada. Spec tetap mencantumkan `minimum: 1` sebagai rentang id yang valid. Endpoint dengan ID publik (`pay_…`, dst.) memvalidasi format dan prefix lewat `internal/pkg/id`.
- Query parameter yang tidak valid ditolak (`400 invalid_request`) di list cursor: `limit` di luar 1–200, enum yang tidak dikenal (rule `oneof`), ID publik yang salah format (rule `public_id`), dan waktu yang bukan RFC 3339 (rule `datetime`). `app_id` yang formatnya benar tapi tidak cocok dengan app mana pun tetap menjadi filter, jadi hasilnya kosong, sama seperti `/admin/payments`.

### Pagination

- List baru memakai cursor: query `limit` (1–200, default 50; di luar rentang → `400 invalid_request`) dan `cursor`; respons `{"data": [...], "next_cursor": "..." | null}`.
- Cursor bersifat opaque: base64url dari JSON berversi yang berisi nilai kolom urutan dan ID publik sebagai tie-breaker, misalnya `(occurred_at, public_id)`. Cursor tidak memuat ID bigint. Cursor rusak → `400 invalid_cursor`.
- Query memakai keyset (`WHERE (sort_col, public_id) < ($1, $2) ORDER BY sort_col DESC, public_id DESC LIMIT n+1`) dengan index yang sesuai, bukan offset.
- Helper ada di `internal/domain/pagination.go` (tipe dan codec cursor) dan transport (parsing query). Semua list admin memakai cursor; tidak ada list offset.

### Audit log

- Tabel `audit_logs`: `public_id` (`aud_`), `occurred_at`, aktor (`actor_type` = `admin` | `system`, `actor_id`), `action` (`{target}.{verb}`, misalnya `app.update`), target (`target_type`, `target_id` sebagai teks), `before`/`after` JSONB, `ip`, `user_agent`, `request_id`.
- Setiap mutasi admin mencatat satu baris di transaksi yang sama dengan perubahannya, lewat `AuditService`. Aktor dibawa lewat `context` (diisi middleware admin; `system` untuk seed, CLI, dan job). Pencatatan tanpa aktor ditolak.
- Snapshot `before`/`after` dibentuk eksplisit per jenis target dan tidak pernah memuat secret: field rahasia dihilangkan, API key hanya 4 karakter terakhir. Sebagai pengaman kedua, key yang namanya (huruf kecil) mengandung `password`, `secret`, atau `token`, atau berakhiran `_enc`, disamarkan menjadi `"[REDACTED]"` di kedalaman mana pun sebelum disimpan.
- Pengecualian redaksi: key seperti secret yang nilainya boolean atau `null` disimpan apa adanya, karena nilai itu tidak bisa membawa secret. Contohnya `must_change_password` di snapshot `admin_user.change_password`. Nilai string, angka, objek, dan array tetap disamarkan. Aturan berbasis tipe nilai ini membuat flag baru tidak perlu didaftarkan satu per satu.
- `actor_type` = `admin` untuk user konsol apa pun role-nya dengan `actor_id` = `usr_…`; `system` untuk seed, CLI, dan setup (`seed`, `cli`, `setup`); `anonymous` (id `anonymous`) untuk login gagal. Entri yang dibuat sebelum tabel `users` ada berisi username admin dan target `admin_user`.
- `GET /admin/audit-logs` (cursor) ditampilkan di Settings.

### Auth, sesi, dan otorisasi

**User.** Tabel `users`: `public_id` (`usr_`), `email` (disimpan sudah di-trim, Unicode NFC, dan huruf kecil; unik lewat index biasa di kolom itu, jadi pencocokan case-insensitive tidak bergantung pada collation Postgres), `name`, `role` (`owner` | `admin` | `viewer`, CHECK), `password_hash` (bcrypt cost 12), `must_change_password`, `disabled_at`, `last_login_at`, `session_version`, `created_at`, `updated_at`. Tidak ada hard delete; "hapus" = disable, supaya `actor_id` di audit log tetap bisa ditelusuri. Password 12 karakter sampai 72 byte (batas bcrypt), di-hash apa adanya tanpa normalisasi.

**Expand/contract `admin_users` → `users`.** Migrasi `000006` membuat `users` dan menyalin setiap baris `admin_users` menjadi `owner` dengan email `<username>@keipay.invalid` (TLD `.invalid` dicadangkan RFC 2606, jadi tidak pernah menjadi alamat orang lain), password dan `must_change_password` dipertahankan. Kode tidak membaca `admin_users`; tabelnya dibiarkan (read-only) dan di-drop di migrasi contract setelah satu rilis. `down` hanya men-drop `users`.

**Sesi.** Cookie `keipay_session` berisi 32 byte acak (base64url), disimpan di Redis dengan key berupa SHA-256 dari nilai cookie, sehingga dump Redis tidak berisi token yang bisa dipakai. Cookie `HttpOnly`, `SameSite=Lax`, `Path=/`, tanpa `Expires`, `Secure` kecuali `COOKIE_SECURE` (default `auto`) mati di `APP_ENV=development`. Batas idle `SESSION_IDLE_TIMEOUT` (30m) dan absolut `SESSION_ABSOLUTE_TIMEOUT` (12h) dicek dari isi sesi, dan TTL Redis mengikuti yang lebih dulu habis. `last_seen_at` diperbarui paling sering sekali per 60 detik. Setiap request terautentikasi tetap memuat user dari Postgres (satu query PK), sehingga disable dan perubahan role langsung berlaku tanpa daftar sesi per user.

**Pencabutan lewat `session_version`.** Sesi menyimpan `session_version` user saat dibuat. Disable, reset password oleh owner, dan ganti password sendiri menaikkan versi itu, sehingga semua sesi lain langsung ditolak (`401 session_expired`) dan habis sendiri oleh TTL. Ganti password sendiri menerbitkan sesi baru untuk browser yang dipakai. Logout menghapus key sesinya. Kalau Redis atau Postgres gagal saat memuat sesi, request admin mendapat `500 internal_error`, bukan `401`: `401` membuat konsol mengarahkan user yang sebenarnya masih login ke `/login` selama gangguan, sedangkan `500` membiarkannya mencoba lagi. Hanya sesi yang memang tidak valid (tidak ditemukan, kedaluwarsa, versi lama, user disabled) yang berujung `401 session_expired`. Pencabutan lewat versi tetap benar walaupun penghapusan key di Redis gagal.

**CSRF.** Double-submit yang diikat ke sesi: cookie `keipay_csrf` (bisa dibaca JS) diterbitkan di respons `/admin/*` mana pun yang request-nya belum membawanya, dan diganti saat login, setup, ganti password, dan logout. Setiap `POST`/`PUT`/`PATCH`/`DELETE` di `/admin/` wajib mengirim header `X-CSRF-Token` yang sama dengan cookie itu, dan kalau ada sesi valid, sama dengan token yang disimpan di sesi (perbandingan constant-time). Ini juga berlaku untuk login dan setup, supaya tidak ada login CSRF. Gagal → `403 invalid_csrf_token`. Konsol memanggil `GET /admin/setup` dulu kalau cookie belum ada.

**Rate limit login.** Counter fixed window di Redis untuk login yang gagal: per IP (default 20) dan per email kanonik (default 5) dalam `LOGIN_FAILURE_WINDOW` (15m). Dicek sebelum password diperiksa, jadi respons `429 rate_limited` + `Retry-After` tidak membocorkan apakah password benar. Login sukses menghapus counter email, tidak counter IP. Hanya kegagalan yang dihitung, supaya banyak admin di balik satu NAT tidak saling mengunci; burst paralel bisa sedikit melewati batas, dan itu diterima. Redis tidak tersedia → login gagal 500 (sesi juga butuh Redis). Setup memakai counter IP yang sama: token setup yang salah dihitung sebagai kegagalan, dan setup yang ditolak batas IP dicatat sebagai security event `auth.setup_rate_limited` (login: `auth.login_rate_limited`). IP klien = peer TCP kecuali proxy terdaftar di `TRUSTED_PROXIES`. Email yang tidak dikenal tetap menjalankan bcrypt terhadap hash dummy supaya waktu respons tidak membocorkan keberadaan akun. User disabled mendapat `401 invalid_credentials` yang sama.

**First-run setup.** `POST /admin/setup` membuat owner pertama hanya kalau tabel `users` kosong, di transaksi dengan `pg_advisory_xact_lock` supaya dua request bersamaan hanya menghasilkan satu owner; berikutnya `409 setup_completed`. `SETUP_TOKEN` opsional di development: kalau di-set, setup wajib menyertakannya. Di `APP_ENV=production` token itu wajib ada: tanpa `SETUP_TOKEN`, setup dijawab `503 setup_token_not_configured` (dicek di dalam transaksi setelah cek `setup_completed`, jadi install yang sudah punya user tidak terpengaruh, dan percobaan itu tidak dihitung sebagai kegagalan). Produksi juga menolak start dengan `COOKIE_SECURE=false`, dan memperingatkan di log kalau `REDIS_PASSWORD` kosong atau DSN Postgres memakai `sslmode=disable`. `make seed` tidak membuat admin. `-reset-admin -email <email> [-password <pw>]` adalah jalur pemulihan dari shell server: set password user itu (acak kalau kosong, dan saat itu `must_change_password` = true), enable kembali, naikkan `session_version`; kalau belum ada user sama sekali, membuat owner.

**Otorisasi.** Permission per role adalah data di `internal/domain` (`RolePermissions`), bukan `if role == ...` yang tersebar. Setiap route admin di `router.go` mendeklarasikan satu permission atau "cukup login"; `GET /admin/me` mengembalikan daftar permission sehingga UI tidak menghardcode matriks. Spec mencatat permission per operasi di `x-keipay-permission`, dan test backend membandingkan route, permission, dan spec supaya tidak drift.

| Permission | owner | admin | viewer |
|---|---|---|---|
| `apps:read`, `connections:read`, `payments:read`, `deliveries:read` | ya | ya | ya |
| `apps:write`, `connections:write`, `reconciliation:write`, `deliveries:replay`, `audit:read` | ya | ya | tidak |
| `users:manage` | ya | tidak | tidak |

Viewer tidak membaca audit log karena isinya (email, IP, perubahan user) setara dengan manajemen user. Permission baru ditambahkan per resource dengan pola yang sama.

Aturan user: selalu minimal satu owner aktif (`409 last_owner` untuk demote atau disable owner aktif terakhir); owner tidak bisa men-disable dirinya sendiri atau me-reset password sendiri lewat API users (`409 cannot_modify_self`). Validasi `role` di `POST`/`PATCH /admin/users`: kosong → rule `required`, nilai di luar `owner`/`admin`/`viewer` → rule `oneof` (keduanya `400 invalid_request`). Semua mutasi user dijalankan di transaksi dengan advisory lock yang sama dengan setup, supaya cek owner terakhir tidak bisa di-race. User dengan `must_change_password` hanya boleh memanggil `GET /admin/me`, `POST /admin/me/password`, dan `POST /admin/logout` (`403 password_change_required`), dicek di server.

Audit: `user.setup`, `user.create`, `user.update`, `user.disable`, `user.enable`, `user.reset_password`, `user.change_password`, `user.login`, `user.login_failed` (actor `anonymous`, `after` berisi email kanonik dan alasan: `invalid_credentials` atau `user_disabled`), `user.logout`. Login yang ditolak rate limit tidak diaudit (hanya security log), supaya audit log tidak bisa dibanjiri.

Sesi tidak memakai JWT. `golang.org/x/text` dipakai untuk normalisasi NFC email.

**Autentikasi `/v1`.** Lihat juga "App API keys" di spec. Dua versi signing: v1 (`"{ts}.{body}"`, signature = nonce) dan v2 opt-in lewat `X-Signature-Version: 2` (`"{ts}.{X-Nonce}.{METHOD}.{path}.{body}"`, nonce klien 16–64 karakter ASCII cetak tanpa spasi; `path` tanpa query). Nonce (v2) atau signature (v1) disimpan di Redis selama `middleware.NonceTTL` (15 menit = 3 × skew ±5 menit) dengan key `nonce:{hash key}:{nonce|signature}`. Cek replay **fail-closed**: Redis tidak menjawab atau nonce store tidak dikonfigurasi → `503 auth_unavailable` (security event `auth.store_unavailable`), karena melewatkan cek replay pada create payment lebih berbahaya daripada meminta app mencoba lagi dengan `Idempotency-Key` yang sama. Kegagalan autentikasi dibatasi lewat `domain.FailureLimiter` (implementasi `redis.FailureCounter` dengan scope `keipay:v1fail:`): unknown key dan env mismatch dihitung per IP, bad signature dan replay per IP dan per pasangan (hash key, IP); counter key sengaja per pengirim, karena counter per key yang global memungkinkan siapa pun yang tahu key sebuah app (tanpa secret-nya) mengunci app itu dari `/v1`, sementara brute force secret 256-bit sudah mustahil tanpa itu. `Blocked` dicek sebelum lookup (hash key dihitung di middleware, jadi pengirim yang diblokir tidak membebani Postgres dan tidak belajar apa pun soal key), `Fail` dicatat setelah kegagalan (bukan reservasi seperti login, supaya request sah yang paralel tidak pernah menahan slot). Batas default 100/IP dan 50/(IP, key) per 15 menit (`V1_AUTH_*`); limiter yang tidak menjawab juga `503`. Di balik reverse proxy, `TRUSTED_PROXIES` wajib diisi; tanpa itu semua caller berbagi alamat proxy dan batas per-IP mengenai semua app sekaligus, yang diperingatkan di log produksi. Yang tidak dihitung: header hilang, timestamp salah, 413, key revoked/expired, app disabled, dan setiap request sukses.

### Apps, API key, dan identifier

Tujuannya: KeiPay selalu tahu sebuah request, payment, atau webhook milik app mana lewat lookup tabel, tidak lewat parsing string.

**Expand/contract `tenants` → `apps`.** Migrasi `000007` membuat tabel baru `apps` dan menyalin setiap baris `tenants` dengan `id` yang sama (sequence `apps` dimajukan sesudahnya), `public_id` `app_<ULID>` dari `created_at`, `environment = live`, `status` dari `active`, dan `webhook_secret_enc` = `api_secret_enc` lama. `payments`, `webhook_deliveries`, dan `ledger_entries` mendapat kolom `app_id` (FK ke `apps`, di-backfill dari `tenant_id`, lalu `NOT NULL`), sedangkan `tenant_id` menjadi nullable dan tidak ditulis. Tabel `tenants` dan kolom `tenant_id` dibiarkan read-only, lalu di-drop di migrasi contract setelah satu rilis, sama seperti `admin_users`. Tabel baru dipakai, bukan `RENAME TABLE`, supaya rollback (`down`) dan binary lama tetap punya data aslinya. Deploy memakai urutan biasa: hentikan, migrate, jalankan. `down` mengembalikan data app ke `tenants` sebisanya; key yang dicabut setelah migrasi hidup lagi dan app baru mendapat `api_key` placeholder yang tidak bisa dipakai, karena `down` hanya alat development.

Tenant lama menjadi `live` karena integrasi yang sudah ada memakai akun Xendit sungguhan.

**App.** `public_id` (`app_`), `slug`, `name`, `environment` (`test` | `live`), `status` (`active` | `disabled`), `metadata` JSONB, `webhook_url`, `redirect_success_url`, `redirect_failure_url`, `webhook_secret_enc`. `slug` unik per environment (`UNIQUE (environment, slug)`), jadi `acme` boleh ada di test dan live. `slug` dan `environment` tidak pernah berubah: `PATCH` yang mengirim salah satunya ditolak dengan rule `immutable`, bukan diabaikan, supaya pemanggil tahu perubahannya tidak terjadi. Disable/enable punya endpoint sendiri (aksi audit sendiri, konfirmasi di UI). Connection memakai kosakata environment yang sama (`test`/`live`), supaya aturan "environment app = environment connection" cukup membandingkan string.

**API key.** Tabel `app_api_keys`: `public_id` (`key_`), `app_id`, `environment`, `label`, `key_prefix` (`kp_test_`, `kp_live_`, atau `kp_` untuk key hasil migrasi), `key_last4`, `key_hash` (hex SHA-256 dari key utuh, `UNIQUE`), `secret_enc` (AES-256-GCM), `last_used_at`, `expires_at`, `revoked_at`. Format key `kp_<env>_` + 32 hex, secret `kps_<env>_` + 48 hex. Key dicari lewat hash-nya, jadi dump database tidak berisi key yang bisa dipakai; key dan secret hanya ditampilkan sekali. Prefix `kps_`/`kpwh_` sengaja tidak memakai `sk_`/`whsec_` supaya secret scanner tidak salah mengira secret KeiPay sebagai secret Stripe.

- **Environment dijaga tiga lapis.** FK komposit `(app_id, environment) → apps (id, environment)` membuat key tidak mungkin tersimpan dengan environment berbeda dari app-nya, `CHECK` mengikat `key_prefix` ke `environment`, dan middleware `/v1` menolak (sebagai `unknown_api_key`, dengan security event) kalau prefix di header tidak cocok dengan environment baris key atau app.
- **Status turunan.** `revoked` kalau `revoked_at` terisi, `expired` kalau `expires_at` sudah lewat, selain itu `active`. Paling banyak 10 key usable per app (dicek di transaksi yang mengunci baris app), supaya rotasi yang lupa mencabut key lama tetap terbatas.
- **Rotasi tumpang tindih.** Rotate membuat key baru dengan label yang sama dan tidak menyentuh key lama; key lama berlaku sampai dicabut atau kedaluwarsa. Alurnya: rotate, pasang key baru di app, cabut key lama. Tidak ada expiry otomatis untuk key lama, karena app yang belum sempat deploy akan putus tanpa peringatan.
- **Urutan cek `/v1`.** Header, timestamp, lookup key (`unknown_api_key`), signature, lalu status key (`api_key_revoked`, `api_key_expired`) dan app (`tenant_inactive`), lalu nonce. Status dicek sesudah signature supaya hanya pemegang secret yang tahu sebuah key dicabut. Code `tenant_inactive` dipertahankan untuk app yang disabled karena code tidak boleh berubah arti atau hilang.
- **Tanpa cache key.** Key dan app tidak di-cache di Redis: satu query `JOIN` key + app lewat index unik `key_hash` per request sudah murah, dan pencabutan berlaku seketika tanpa invalidasi. `last_used_at` diperbarui dengan `UPDATE … WHERE last_used_at IS NULL OR last_used_at < now() - interval '1 minute'`, best effort: kegagalannya hanya dicatat di log.

**Webhook signing secret terpisah dari API key.** Karena satu app punya banyak key dengan secret berbeda, webhook keluar tidak ditandatangani dengan secret API key, melainkan dengan secret milik webhook endpoint (`kpwh_` + 48 hex, lihat [Webhook router](#webhook-router)). Rotasi API key tidak pernah memutus verifikasi webhook. Endpoint hasil migrasi memakai `api_secret` lama app-nya, jadi verifikasi `X-Kei-Signature` di integrasi yang sudah ada tidak berubah.

**Payment.** `payments` mendapat `public_id` (`pay_`, backfill dari `created_at`), `app_id`, `metadata` JSONB (default `{}`), `idempotency_key`, dan `idempotency_fingerprint`. Index unik baru `(app_id, order_id)` dan `(app_id, idempotency_key) WHERE idempotency_key IS NOT NULL`, jadi dua app boleh memakai `order_id` atau key yang sama.

- **Reference ke gateway** untuk payment baru adalah `kp_<public_id>` (misalnya `kp_pay_01JB…`), disimpan di `payments.external_id` dan dikirim sebagai `external_id` Xendit. Callback dicari dengan `WHERE external_id = $1` lewat index unik yang sudah ada. Payment lama yang memakai `{slug}:{order_id}` ditemukan dengan query yang sama karena nilainya tersimpan di kolom itu; tidak ada kode yang mem-parse slug. Callback yang reference-nya tidak ditemukan dijawab `500` supaya Xendit mengulang. Rencana routing multi-gateway memindahkan reference ke `payment_attempts`, dan lookup ini ikut pindah.
- **Idempotency.** Kalau `Idempotency-Key` dikirim, key dicek lebih dulu: key sama dan fingerprint sama → payment lama (`201` + `Idempotent-Replayed: true`); key sama dengan fingerprint berbeda → `409 idempotency_key_reused`. Fingerprint = hex SHA-256 dari JSON kanonik input yang sudah divalidasi (`order_id`, `amount`, `currency`, `description`, `payer_email`, `invoice_duration`, `items`, `customer`, `expected_settlement_at`, `metadata`), bukan raw body, supaya urutan key JSON atau spasi tidak membuat retry yang sah ditolak. Kalau tidak ada key yang cocok, aturan `order_id` berlaku (amount dan currency sama → payment yang ada, beda → `409 conflict`), sehingga integrasi tanpa `Idempotency-Key` tetap berperilaku sama. Request ber-key yang jatuh ke payment lama lewat `order_id` tidak menempelkan key-nya ke payment itu. Key tidak kedaluwarsa. Pembuatan invoice: buat di Xendit, lalu insert; kalah race → expire invoice yatim dan kembalikan pemenangnya kalau cocok.
- **Metadata** (payment dan app) adalah objek datar: maksimal 50 key, key 1–40 code point tanpa karakter kontrol, nilai string (maksimal 500 code point, tanpa U+0000 karena Postgres JSONB menolaknya), angka, boolean, atau `null`, total maksimal 8192 byte JSON kompak. Angka di-decode dengan `json.Number` supaya tidak dibulatkan lewat float; urutan key tidak dijaga (JSONB mengurutkan ulang). Objek datar dipilih karena cukup untuk tag dan ID eksternal, mudah ditampilkan di konsol, dan bisa dilonggarkan nanti tanpa breaking change. Metadata payment ikut dikirim di setiap webhook payment itu.
- **Kontrak `/v1` hanya bertambah.** Respons payment membawa `id` (`pay_…`), `reference`, dan `metadata`, di samping `payment_id` integer. Payload webhook v1 membawa `payment_public_id` dan `metadata`. Envelope berversi dijelaskan di [Webhook router](#webhook-router).

**Admin.** `/admin/apps`, `/admin/apps/{id}`, disable/enable, dan `/admin/apps/{id}/keys` (list, create, rotate, revoke). `/admin/payments` memakai cursor dengan filter `app_id`, `status`, `from`, `to` yang divalidasi ketat dan tanpa `total` (count penuh mahal dan tidak dibutuhkan UI). Baris payment, payment report, dan delivery membawa ringkasan app (`{id, slug, name, environment}`) hasil `JOIN`, jadi konsol tidak perlu memuat semua app untuk menampilkan nama.

Audit: `app.create` dan `api_key.create` (dua baris di satu transaksi saat app dibuat), `app.update` (hanya field yang berubah), `app.disable`, `app.enable`, `api_key.create`, `api_key.rotate` (target key baru, `before` key lama, `after` key baru), `api_key.revoke`. Snapshot key berisi `id`, `app_id`, `label`, `environment`, `prefix`, `last4`, `expires_at`, `revoked_at`, tanpa key utuh, hash, maupun secret. Entri `tenant.*` dari sebelum migrasi `apps` tidak diubah.

**Konsol.** Switch Test/Live di top bar menjadi filter environment awal di list Apps (chip environment tetap bisa diganti ke All), dan dialog buat app memilih environment sesuai mode. List Connections dan wizard tambah connection melakukan hal yang sama. Webhooks dan Reconciliation difilter per mode lewat join ke `apps.environment` / `connections.environment`, bukan kolom environment baru di `payments`. Halaman Payments belum difilter per mode.

### Connections dan provider

Tujuannya: menambah gateway cukup dengan satu paket adapter dan satu baris registrasi, tanpa mengubah frontend.

**Port.** `internal/provider` berisi port `Provider`, `Registry`, `Capabilities`, schema field, dan kelas error (`ErrRejected`, `ErrRetryable`, `ErrUnknownOutcome`, `ErrNotFound`, dibungkus dengan `%w` dan dicek dengan `errors.Is`). Bentuk port-nya ada di [Port `provider.Provider`](#port-providerprovider).

- Setiap call ke PG menerima `provider.Account{Environment, Credentials, Settings}`, karena setting connection (misalnya durasi invoice default) ikut menentukan request.
- Adapter mendeklarasikan `CredentialSchema()` dan `SettingsSchema()` dalam satu format (`SchemaField`: `key`, `type` `string|integer|boolean|enum`, `label`, `description`, `placeholder`, `secret`, `required`, `default`, `options`, `min_length`, `max_length`, `pattern` + `pattern_hint`, `minimum`, `maximum`). Validator generik di `internal/provider` dipakai service untuk create dan update; `ValidateAccount(env, creds)` di adapter menambah aturan lintas field (Xendit: prefix `xnd_development_` hanya untuk `test`, `xnd_production_` hanya untuk `live`). Key yang tidak ada di schema ditolak (`unknown`), supaya salah ketik tidak tersimpan diam-diam. Nilai kredensial selalu string dan di-trim.
- `Descriptor()` mengembalikan nama tampilan, deskripsi, URL dokumentasi, dan langkah memasang URL webhook di dashboard PG. `GET /admin/providers` menyajikan semuanya; konsol tidak punya kode per provider.
- Rekonsiliasi tidak memakai `FindByReference` atau `GetStatus` (rencana): ia menarik daftar transaksi lewat interface opsional `provider.TransactionLister` (lihat [Rekonsiliasi](#rekonsiliasi)).

**Data.** Migrasi `000008`:

- `merchants`: satu baris default (`is_default`, unik parsial), tanpa ID publik dan tidak tampil di UI. `legacy_connection_id` menunjuk connection hasil impor env, target alias `/webhooks/xendit`.
- `connections`: `public_id` (`con_`), `merchant_id`, `provider`, `name` (format seperti slug, 3–64 karakter, unik per merchant, boleh diganti), `environment` (`test` | `live`, kosakata yang sama dengan app), `status` (`active` | `disabled`), `credentials_enc`, `credentials_dek_enc`, `key_version`, `credentials_last4` JSONB, `settings` JSONB, `last_tested_at`, `last_test_status` (`ok` | `failed`), `last_test_error`. `UNIQUE (id, merchant_id, environment)` menjadi target FK komposit dari `apps`.
- `connection_channels`: `(connection_id, channel)`, `enabled`. Fee, batas amount, dan jadwal maintenance per channel (lihat [Data Models](#data-models)) ditambahkan secara additive bersama fitur yang memakainya, supaya tidak ada konfigurasi yang tersimpan tanpa efek.
- `apps.merchant_id` (backfill ke merchant default, lalu `NOT NULL`) dan `apps.default_connection_id`, dengan FK komposit `(default_connection_id, merchant_id, environment) → connections (id, merchant_id, environment)`. Jadi app tidak mungkin menunjuk connection milik merchant lain atau beda environment, bahkan lewat SQL langsung. Service tetap memeriksa lebih dulu supaya error-nya berupa field error yang jelas.
- `payments.connection_id` dan `payments.provider` (nullable). Payment baru selalu mengisinya; payment lama diisi saat impor env.

**Envelope encryption.** Setiap connection punya data key acak 32 byte. Kredensial (JSON) dienkripsi AES-256-GCM dengan data key itu dan `public_id` connection sebagai additional data, sehingga ciphertext yang dipindah ke baris lain gagal didekripsi. Data key dienkripsi dengan master key `ENCRYPTION_KEY` lewat `crypto.Cipher` yang sudah ada. `key_version = 1`; rotasi master key cukup mengenkripsi ulang data key. Respons admin hanya membawa 4 karakter terakhir per field (`null` kalau nilainya kurang dari 12 karakter, karena 4 karakter dari nilai sependek itu terlalu banyak membuka), disimpan di `credentials_last4` saat ditulis sehingga list dan detail tidak perlu dekripsi. Snapshot audit tidak pernah memuat kredensial maupun last4: `connection.update_credentials` hanya mencatat nama field yang diganti.

**Impor env (sekali).** Kalau `XENDIT_SECRET_KEY` dan `XENDIT_CALLBACK_TOKEN` di-set dan merchant default belum punya `legacy_connection_id`, server saat start membuat satu connection Xendit (nama `xendit-imported`, atau `xendit-imported-<6 karakter terakhir ID>` kalau nama itu sudah dipakai), dengan environment dari prefix key, lalu dalam transaksi yang sama: mengisi `legacy_connection_id`, mengisi `payments.connection_id`/`provider` yang masih kosong, dan menjadikannya default connection untuk setiap app yang belum punya default dan environment-nya sama. Transaksi mengunci baris merchant, jadi dua instance yang start bersamaan hanya mengimpor sekali. Impor dicatat di audit sebagai `system:import` (`connection.create`, dan `app.update` per app). Setelah itu env diabaikan, dan server memberi peringatan setiap start supaya env dihapus. Kalau hanya satu dari dua env yang di-set, config gagal dimuat. Key dengan prefix yang tidak dikenal tidak diimpor (error di log, server tetap jalan). App hasil migrasi `tenants` (`live`) tidak dipindah environment-nya walaupun key-nya `xnd_development_`: memindah environment app juga harus memindah semua API key-nya (FK komposit) dan mengubah arti key yang sudah dipakai integrasi. App seperti itu tidak mendapat default connection, dan server mencatat peringatan berisi daftar app tersebut. Konsekuensinya: deploy yang sudah ada harus tetap menyetel kedua env itu saat start pertama setelah upgrade.

**Webhook masuk per connection.** `POST /webhooks/{provider}/{connection_id}` me-resolve connection lewat ID publik, mewajibkan provider di path sama dengan provider connection (selain itu `404`), lalu `VerifyWebhook` dengan kredensial connection itu sebelum body dibaca. Token yang valid untuk connection lain ditolak `401 invalid_callback_token` dan dicatat sebagai security event. Payment hasil lookup reference harus milik connection yang sama; kalau tidak, callback dicatat tetapi tidak di-apply (security event, `200` ke PG), sama seperti mismatch amount. Connection yang disabled tetap menerima callback supaya payment lama tetap ter-update. `/webhooks/xendit` adalah alias ke `legacy_connection_id` (`404` kalau tidak ada), sehingga dashboard Xendit yang belum diubah tetap jalan. Dedupe event berlaku per connection (lihat [Webhook router](#webhook-router)). Satu akun PG sebaiknya hanya punya satu connection: ganti kredensial di connection yang sama, jangan membuat connection kedua untuk akun yang sama, karena PG hanya mengirim callback ke satu URL dan payment milik connection lama akan dianggap mismatch.

**Create payment.** Replay idempotent dijawab sebelum connection dilihat. Sesudah itu payment memakai `default_connection_id` app: tidak ada → `422 no_eligible_connection` (`reasons: ["no_default_connection"]`), disabled → `["connection_disabled"]`. Kalau channel yang aktif adalah subset ketat dari channel provider, checkout dibatasi ke channel itu; kalau semua aktif atau belum diatur, checkout tidak dibatasi, karena mengirim daftar lengkap bisa membuat Xendit menolak metode yang belum diaktifkan di akun. Error adapter dipetakan: `ErrRejected` → `422 provider_rejected`, `ErrRetryable` dan `ErrUnknownOutcome` → `503 provider_unavailable`. Pesan PG hanya masuk log. `ErrUnknownOutcome` saat create masih bisa meninggalkan invoice di Xendit tanpa baris payment; attempt dan adopsi (rencana routing multi-gateway) menutup celah ini.

**Test connection.** `POST /admin/connections/{id}/test` memanggil `TestCredentials` (Xendit: list satu invoice, permission yang memang dibutuhkan) dengan timeout 10 detik dan selalu menjawab `200` dengan `result` `ok`/`failed` plus pesan yang sudah disanitasi. Hasilnya disimpan di connection dan dicatat `connection.test`. Mengganti kredensial mengosongkan hasil test.

**Otorisasi.** `connections:read` (owner, admin, viewer) dan `connections:write` (owner, admin). Memilih default connection app memakai `apps:write`. Disable connection ditolak `409 connection_in_use` selama ada app (aktif maupun disabled) yang memakainya sebagai default, supaya default connection app selalu aktif.

Audit: `connection.create`, `connection.update` (hanya field yang berubah), `connection.update_credentials`, `connection.update_channels`, `connection.disable`, `connection.enable`, `connection.test`, dan `app.update` untuk `default_connection_id`. Snapshot connection berisi `id`, `provider`, `name`, `environment`, `status`, `settings`, tanpa kredensial.

**Konsol.** Wizard tambah connection adalah halaman (`/connections/new`), bukan dialog, supaya nyaman di mobile: provider dan environment → form kredensial dan setting dari schema → connection dibuat lalu langsung dites → URL webhook + petunjuk provider → channel. Langkah setelah connection dibuat boleh dilewati karena semuanya bisa diatur di halaman detail. Form dirender generik dari `ConfigSchema`; field secret memakai input password dengan tombol tampilkan dan tidak pernah diisi ulang dari server (saat ganti kredensial, field kosong berarti "tetap").

### Webhook router

Tujuannya: setiap app menentukan sendiri ke mana dan event apa saja yang dikirim KeiPay, dan setiap webhook masuk dari PG bisa ditelusuri.

**Katalog event.** Daftar event adalah data di `internal/domain` (`EventCatalog`: tipe, deskripsi, contoh `data`), disajikan lewat `GET /admin/event-types` beserta wildcard yang diterima. Konsol membangun pemilih event dari respons itu, tidak menghardcode daftar. Tipe: `payment.paid`, `payment.settled`, `payment.expired`, `payment.failed` (dari `"payment." + lower(status)`), dan `payment.duplicate`. `payment.duplicate` ada di katalog supaya app bisa subscribe, tetapi belum dikirim: hanya attempt loop (rencana routing multi-gateway) yang bisa tahu sebuah payment dibayar dua kali, yaitu saat attempt yang bukan attempt aktif dibayar.

**Subscription.** Setiap endpoint menyimpan daftar (`TEXT[]`, 1–20 entri) berisi tipe persis, wildcard grup (`payment.*`, hanya untuk grup yang ada di katalog), atau `*` (semua tipe, termasuk tipe baru di masa depan). Pencocokan dilakukan di Go (`domain.MatchSubscription`), bukan SQL, karena jumlah endpoint per app kecil (maksimal 10).

**Endpoint (`webhook_endpoints`, `whe_`).** Kolom: `app_id`, `url`, `description`, `events`, `api_version` (`v1` | `v2`), `legacy_signature`, `secret_enc` + `secret_last4`, `previous_secret_enc` + `previous_secret_expires_at`, `headers_enc` (JSON `[{name, value}]` terenkripsi) + `headers_hint` (JSONB `[{name, value_last4}]` untuk tampilan tanpa dekripsi), `status` (`active` | `disabled`), `disabled_reason` (`manual` | `consecutive_failures`), `disabled_at`, `consecutive_failures`, `retry_max_attempts`, `retry_backoff`, `retry_interval_seconds`, `migrated`, `deleted_at`. Maksimal 10 endpoint yang belum dihapus per app (`400 validation_failed`, sama seperti batas API key). Hapus = soft delete (`deleted_at`), supaya delivery lama tetap bisa dibaca dan audit tetap bisa ditelusuri. URL mengikuti aturan `HttpUrl`; app `live` wajib `https` (rule `https`). Alamat privat tidak diblokir: KeiPay self-hosted dan penerimanya sering ada di jaringan internal, dan URL hanya bisa diisi admin.

- **Header kustom** (maksimal 10) disimpan terenkripsi karena nilainya sering berupa token (`Authorization`). Respons hanya membawa nama dan 4 karakter terakhir (`null` kalau nilainya kurang dari 12 karakter), dan PATCH mengganti seluruh daftar. Nama yang dipakai KeiPay sendiri (`Content-Type`, `Content-Length`, `Host`, `User-Agent`, `Connection`, `Transfer-Encoding`, `X-KeiPay-*`, `X-Kei-*`) ditolak (rule `reserved`). Log attempt menampilkan nilainya sebagai `[REDACTED]`.
- **Secret** `kpwh_` + 48 hex, dienkripsi dengan `crypto.Cipher` yang sama dengan secret app lama (tanpa additional data), sehingga migrasi bisa menyalin ciphertext `apps.webhook_secret_enc` apa adanya. Endpoint hasil migrasi tidak punya `secret_last4` (plaintext tidak tersedia di SQL) sampai secret-nya dirotasi.
- **Rotasi tumpang tindih.** Rotate membuat secret baru (ditampilkan sekali), memindahkan secret lama ke `previous_secret_*` dengan masa berlaku `overlap_hours` (0–168, default 24), dan membuang previous yang lebih lama. Selama masa itu `X-KeiPay-Signature` membawa dua `v1=` (baru dulu), jadi penerima yang masih memakai secret lama maupun yang sudah memakai secret baru sama-sama lolos. `X-Kei-Signature` hanya satu nilai, jadi selalu memakai secret terbaru; penerima yang masih memakainya sebaiknya pindah ke `X-KeiPay-Signature` sebelum rotasi.
- **Kebijakan retry** per endpoint: `max_attempts` 1–20 (default 8), `backoff` `exponential` (`interval × 2^(n-1)`, maksimal 6 jam) atau `fixed`, `interval_seconds` 10–3600 (default 30). Default ini sekitar satu jam total. Tanpa jitter: jumlah delivery per endpoint kecil dan perilaku yang deterministik lebih mudah dites dan dijelaskan.

**Event dan envelope.** Setiap perubahan status yang di-apply membuat satu baris `events` (`evt_`, `type`, `api_version` = versi `data` = `v2`, `app_id`, `payment_id`, `data` JSONB, `is_test`) di transaksi yang sama dengan update payment. Envelope `v2` adalah `{id, type, api_version, created_at, data}` dengan `data` = snapshot payment (`id`, `order_id`, `reference`, `status`, `amount`, `currency`, `metadata`, `provider`, `connection_id`, `paid_at`, `settled_at`). Field `channel` dan `provider_fee` ditambahkan ke `data` secara additive saat datanya tersedia.

- **Body per versi.** Endpoint `v2` menerima envelope; endpoint `v1` menerima body datar lama (`PaymentWebhookEvent`) ditambah `event_id`. Body dirender sekali saat fan-out sesuai `api_version` endpoint saat itu dan disimpan di `webhook_deliveries.payload`, sehingga setiap attempt dan replay mengirim byte yang sama, dan detail delivery menunjukkan persis apa yang dikirim. Mengganti `api_version` hanya berlaku untuk delivery berikutnya.
- **Header.** `X-KeiPay-Event-Id`, `X-KeiPay-Event-Type`, `X-KeiPay-Delivery-Id`, `X-KeiPay-Signature: t=<ts>,v1=<hex HMAC-SHA256("{ts}.{body}")>`, `User-Agent: KeiPay-Webhooks/1`, dan `X-KeiPay-Test: true` untuk kirim uji. Endpoint dengan `legacy_signature` juga mendapat `X-Kei-Signature` (HMAC body saja) dan `X-Kei-Event`. Timestamp ada di dalam header signature, bukan header terpisah, supaya tidak bisa dipisah dari tanda tangannya.

**Fan-out.** `WebhookService.Process` memuat endpoint aktif (belum dihapus) milik app dalam satu query, mencocokkan subscription, lalu membuat satu `webhook_deliveries` per endpoint yang cocok, di transaksi yang sama. Setelah commit semua delivery di-enqueue. App tanpa endpoint yang cocok tetap mendapat baris `events` (berguna untuk timeline payment), tanpa delivery.

**Delivery (`webhook_deliveries`, expand).** Tabel lama diperluas, tidak dibuat ulang: `public_id` (`dlv_`, backfill dari `created_at`), `event_id`, `endpoint_id`, `max_attempts`, `next_attempt_at`, `last_attempt_at`, `last_response_status`; `payment_id` menjadi nullable karena kirim uji tidak punya payment. Status `PENDING` | `DELIVERED` | `FAILED`: `PENDING` juga berarti "menunggu retry" (`next_attempt_at`), dan `FAILED` hanya berarti selesai gagal. Setiap attempt dicatat di `webhook_delivery_attempts` (nomor, manual atau tidak, URL, header request dengan nilai kustom disamarkan, status dan 4096 byte pertama body respons, error, durasi). Retensi log belum ada.

- **Retry dikendalikan KeiPay, bukan asynq.** Worker mengirim, mencatat attempt, lalu sendiri menjadwalkan attempt berikutnya (`next_attempt_at`, task asynq dengan `ProcessAt`) atau menandai `FAILED`. Task ID `webhook:forward:{delivery_id}:{attempt}` membuat setiap attempt unik, dan worker mengabaikan task yang nomor attempt-nya tidak sama dengan `attempts + 1` (task basi). Update hasil attempt memakai guard `WHERE attempts = $n AND status = 'PENDING'`, jadi dua worker tidak bisa mencatat attempt yang sama. Error yang dikembalikan ke asynq hanya untuk gangguan infrastruktur (database), dengan `MaxRetry` 10. Alasannya: kebijakan retry per endpoint, jadwal yang terlihat di UI, dan replay yang tidak tergantung pada task yang sudah diarsipkan asynq.
- **Respons yang dihitung sukses** hanya `2xx` dalam 15 detik. Redirect tidak diikuti (dihitung gagal), supaya signature dan header kustom tidak terkirim ke host lain.
- **Sweeper** mengambil delivery `PENDING` yang `next_attempt_at`-nya sudah lewat lebih dari 2 menit (enqueue setelah commit gagal, Redis di-flush, atau task diarsipkan), lalu enqueue ulang dengan task ID yang sama. Kalau ID itu masih dipegang task yang diarsipkan, task itu dihapus lewat `asynq.Inspector` lalu di-enqueue lagi. Sweeper memakai index `idx_deliveries_due (next_attempt_at) WHERE status = 'PENDING'`.
- **Nonaktif otomatis.** `consecutive_failures` di endpoint naik setiap kali sebuah delivery habis attempt-nya, dan kembali 0 pada attempt mana pun yang sukses. Saat mencapai `WEBHOOK_DISABLE_AFTER_FAILURES` (default 5; 0 = tidak pernah), endpoint dinonaktifkan dengan `disabled_reason = consecutive_failures` di transaksi yang sama dan dicatat di audit sebagai `system:worker` (`webhook_endpoint.auto_disable`). Dihitung per delivery, bukan per attempt, supaya satu event yang gagal 8 kali tidak langsung mematikan endpoint. Delivery `PENDING` milik endpoint yang disabled atau dihapus menjadi `FAILED` saat gilirannya tiba, tanpa attempt. Enable mengembalikan `consecutive_failures` ke 0; delivery yang gagal tidak dikirim ulang otomatis.
- **Replay** hanya untuk delivery `DELIVERED` atau `FAILED`: `max_attempts = attempts + 1`, status `PENDING`, satu attempt manual tanpa retry, dan tidak memengaruhi `consecutive_failures`. Event id, delivery id, dan body tetap sama, jadi replay idempotent dari sisi penerima yang dedupe per `event_id`. Replay tidak membuat event atau delivery baru. Selain itu ditolak `409 delivery_not_replayable` dengan `details.reason` (`in_progress`, `no_endpoint`, `endpoint_deleted`, `endpoint_disabled`).
- **Kirim uji** membuat event `is_test` dengan `data` dari contoh katalog dan satu delivery ke endpoint itu saja (tanpa melihat subscription maupun status), `max_attempts = 1`, tidak memengaruhi `consecutive_failures`, dan membawa `X-KeiPay-Test: true`.

**Migrasi `apps.webhook_url` (expand).** Migrasi `000009` membuat satu endpoint per app yang punya `webhook_url`: subscribe `*`, `api_version = v1`, `legacy_signature = true`, `migrated = true`, `secret_enc` = `apps.webhook_secret_enc`. Integrasi yang sudah ada tetap menerima body `v1` dan `X-Kei-Signature`, ditambah header `X-KeiPay-*`. Delivery lama mendapat `public_id`, `endpoint_id` = endpoint hasil migrasi app-nya (kalau ada), dan `max_attempts`; `event_id` tetap `NULL` karena dibuat sebelum ada events. Delivery lama yang masih `PENDING` mendapat `next_attempt_at = now()` dan sisa enam attempt, sehingga sweeper mengirimnya dengan mekanisme baru; task asynq lama (`{"delivery_id": n}`, tanpa nomor attempt) tetap diterima worker. `apps.webhook_url` dan `apps.webhook_secret_enc` tidak dibaca maupun ditulis (kolom secret mendapat default `''`) dan di-drop di migrasi contract setelah satu rilis. API admin app menolak `webhook_url` (rule `moved`) dan create app tidak mengembalikan `webhook_secret`.

**Webhook masuk (`inbound_webhooks`, `inb_`).** Setiap callback ke connection yang dikenal dicatat: `connection_id`, `provider`, `source` (`webhook`; `poll` untuk reconciler status yang direncanakan), `signature_valid`, `result`, `result_detail`, `event_key`, `provider_status`, `reference`, `payment_id`, `event_id`, `raw_body` (`BYTEA`, byte persis seperti diterima), `body_truncated`, `received_at`. Callback ke connection yang tidak dikenal (`404`) tidak dicatat supaya tabel tidak bisa dibanjiri lewat URL tebakan; body yang gagal verifikasi disimpan maksimal 4096 byte dan body terverifikasi yang tidak bisa di-parse maksimal 64 KiB (`body_truncated` = true) untuk alasan yang sama; body yang diterapkan disimpan utuh. Body di atas 1 MiB ditolak `413 request_too_large` sebelum dibaca habis. `result`: `applied`, `stale`, `duplicate`, `mismatch`, `unmapped`, `invalid_signature`, `invalid_payload`, `error`.

- **Batas callback yang gagal verifikasi.** Callback yang gagal verifikasi dihitung di Redis per IP pengirim (`CALLBACK_MAX_REJECTED_PER_IP`, default 60) dan per connection (`CALLBACK_MAX_REJECTED_PER_CONNECTION`, 60) dalam `CALLBACK_REJECTED_WINDOW` (15m), di `WebhookService.RecordRejected` dengan `ClientMeta` dari context request. Callback yang lolos verifikasi tidak pernah dibatasi, bisa di-parse atau tidak: ia datang dari akun gateway sendiri, dan barisnya adalah satu-satunya bukti masalah format; kalau ikut dihitung, orang asing yang berbagi alamat dengan gateway (NAT, proxy tanpa `TRUSTED_PROXIES`) bisa menghilangkannya. Di atas batas, callback yang ditolak dijawab `429 rate_limited` + `Retry-After` tanpa baris log (security event `callback.rate_limited`). Verifikasi tetap dijalankan dulu, jadi callback yang sah dari pengirim atau connection yang sedang diblokir tetap diproses: batas ini hanya melindungi tabel, bukan menolak gateway. Kalau Redis tidak menjawab, callback tetap dicatat (fail-open) karena retry PG lebih penting daripada batasnya dan dedupe ada di Postgres; berbeda dari `/v1`, yang menolak `503` karena cek replay-nya tidak boleh dilewati.

- **Dedupe ada di tabel ini, per connection.** Index unik parsial `(connection_id, event_key, provider_status) WHERE result IN ('applied','stale','mismatch','unmapped')`. Di dalam transaksi proses, baris diklaim lebih dulu (`INSERT … ON CONFLICT DO NOTHING`); kalah klaim berarti `duplicate` (dicatat sebagai baris tersendiri, `200`). Hasil akhir di-update di transaksi yang sama. Kalau transaksi gagal (`500`, misalnya reference tidak dikenal), klaimnya ikut batal sehingga retry PG diproses penuh, dan satu baris `error` dicatat di luar transaksi (best effort). Karena dedupe per connection, callback dari connection lain untuk reference yang sama boleh dicatat sebagai `mismatch` tanpa membuat callback asli terlihat duplikat.
- **Tanpa dedupe di Redis.** Setiap callback dicatat ke database, jadi lapisan dedupe di Redis tidak menghemat apa pun. Tabel lama `webhook_events` tidak ditulis dan di-drop di migrasi contract. Callback yang tercatat di `webhook_events` sebelum upgrade lalu diulang PG sesudahnya diproses lagi, tetapi state machine menjadikannya `stale`, jadi tidak ada forward ganda.
- **Status PG yang tidak dikenal** dicatat `unmapped`: adapter mengembalikan `InboundEvent.Status` kosong untuk status di luar tabelnya, sesuai properti 8.

**API admin.** `GET /admin/event-types`; `GET/POST /admin/apps/{id}/webhook-endpoints`; `GET/PATCH/DELETE /admin/webhook-endpoints/{id}` dan `rotate-secret`, `disable`, `enable`, `test` di bawahnya (operasi per endpoint tidak bersarang di app karena `whe_` sudah unik global); `GET /admin/webhook-deliveries` (cursor, filter `app_id`, `endpoint_id`, `event_id`, `event_type`, `status`, `environment`, `from`, `to`), `GET /admin/webhook-deliveries/{id}` (body + log attempt), replay dengan `dlv_`; `GET /admin/inbound-webhooks` (cursor, filter `connection_id`, `result`, `environment`, `from`, `to`) dan detailnya dengan raw body. Path `/admin/inbound-webhooks` sejajar dengan `/admin/webhook-deliveries`. List tidak membawa body; body hanya di detail.

**Otorisasi.** Endpoint adalah setting app: `apps:read` dan `apps:write`, termasuk katalog event. `deliveries:read` mencakup log keluar dan masuk, `deliveries:replay` untuk replay. Tidak ada permission baru.

Audit: `webhook_endpoint.create`, `.update` (hanya field yang berubah; header hanya nama), `.delete`, `.rotate_secret` (`after`: `secret_last4`, `previous_secret_expires_at`), `.disable`, `.enable`, `.auto_disable` (`system:worker`), `.test`, dan `webhook_delivery.replay` dengan target `dlv_…`. Snapshot endpoint berisi `id`, `app_id`, `url`, `description`, `events`, `api_version`, `legacy_signature`, `header_names`, `status`, `disabled_reason`, `retry_policy`, tanpa secret maupun nilai header.

**Konsol.** Tab Webhooks di detail app: daftar endpoint, tambah/edit (pemilih event dari katalog, header kustom, retry, versi body), secret reveal-once, rotasi dan hapus dengan konfirmasi mengetik host URL, kirim uji, dan status nonaktif otomatis beserta alasannya. Halaman Webhooks global: tab Outbound dan Inbound dengan filter chip, cursor, dan detail di panel samping. Karena semua path di bawah `/webhooks/` dicadangkan untuk callback PG, tab dan detail memakai query string (`/webhooks?tab=inbound&id=inb_…`), bukan path. Kedua tab difilter mode Test/Live lewat `environment` (join ke `apps.environment` dan `connections.environment`).

### Rekonsiliasi

KeiPay tidak memegang dana, jadi rekonsiliasi berarti membandingkan catatan KeiPay dengan catatan PG, per connection dan per periode, lalu menyimpan setiap perbedaan supaya bisa ditelusuri dan diselesaikan.

**Payment report (expand/contract `ledger_entries` → `payment_reports`).** Migrasi `000010` membuat tabel baru `payment_reports`, satu baris per payment yang sukses (`UNIQUE (payment_id)`): `app_id`, `connection_id` (nullable untuk payment lama), `currency`, `gross`, `fee` dan `net` (keduanya `NULL` kalau PG tidak melaporkan fee; CHECK `net = gross - fee`), `status` (`PAID` | `SETTLED`), `paid_at` (`NOT NULL`), `settled_at`. Backfill dari `ledger_entries` bertipe `PAYMENT` join `payments`. Semua fee lama di-backfill sebagai tidak diketahui (`NULL`), karena nilai `xendit_fee` di `ledger_entries` berasal dari `fees_paid_amount` callback, yaitu fee yang dibebankan ke customer, bukan potongan Xendit dari merchant. Memakainya akan memunculkan `fee_mismatch` palsu. Kode hanya menulis `payment_reports` (di transaksi yang sama dengan perubahan status, upsert per payment: status hanya maju, `settled_at` dan fee pertama yang diketahui dipertahankan). `ledger_entries` dibiarkan read-only dan di-drop di migrasi contract setelah satu rilis, sama seperti `tenants` dan `webhook_events`. `down` menyalin baris report kembali ke `ledger_entries` (`ON CONFLICT DO NOTHING`) sebelum men-drop tabel baru. Tabel baru dipakai, bukan `RENAME`, supaya binary lama dan rollback tetap punya data aslinya. Refund tidak punya report (di luar cakupan v1).

`provider.InboundEvent.Fee` bertipe `*int64`, sehingga "tidak diketahui" dan "nol" bisa dibedakan. Fee yang dicatat adalah potongan PG dari merchant. Untuk Xendit: `paid_amount` (atau `amount` kalau kosong) dikurangi `adjusted_received_amount`, dan `nil` kalau callback tidak membawa `adjusted_received_amount`. `fees_paid_amount` adalah fee yang dibebankan ke customer (biasanya 0 di invoice biasa) dan tidak dibaca.

**Matcher independen sumber.** Matcher adalah fungsi murni di service: input berupa baris KeiPay dan baris transaksi PG yang sudah dinormalisasi (`domain.GatewayTransaction`: reference, id transaksi PG, status asli dan status normal, amount, currency, fee opsional, waktu), output berupa item. Sumber baris PG adalah interface `TransactionSource`; implementasi sekarang menarik lewat adapter, dan upload CSV nanti cukup menjadi implementasi kedua tanpa mengubah matcher.

- Port: interface opsional `provider.TransactionLister` (`ListTransactions(ctx, Account, TransactionQuery{From, To, Cursor, Limit}) (*TransactionPage, error)`), plus `Capabilities.ListTransactions`. `NewRegistry` menolak adapter yang mendeklarasikan `ListTransactions` tanpa mengimplementasikan `TransactionLister`. Konsol hanya menawarkan connection yang provider-nya `list_transactions`.
- Xendit: `GET /transactions?types=PAYMENT&created[gte]=…&created[lte]=…&limit=…&after_id=…` dengan HTTP biasa (basic auth secret key), **bukan** SDK, karena model SDK v6.4.0 memakai `float32` untuk amount dan fee sehingga nominal di atas sekitar Rp16,7 juta kehilangan presisi. Angka di-decode sebagai `json.Number` lalu dikonversi ke minor unit menurut eksponen ISO 4217 (IDR 0). `reference_id` = reference KeiPay, `id` = id transaksi PG; status `SUCCESS` → `PAID` (`SETTLED` kalau `settlement_status = SETTLED`), `PENDING` → `PENDING`, `FAILED`/`VOIDED`/`REVERSED` → `FAILED`; fee = `xendit_fee + value_added_tax`, angka yang sama dengan fee dari callback (gross dikurangi `adjusted_received_amount`). Pemetaan ini dikunci dengan fixture di test adapter.
- Halaman PG yang gagal `ErrRetryable` diulang sampai 3 kali (jeda 1 dan 2 detik) sebelum run gagal. Batas 100.000 transaksi per run (`too_many_transactions`), supaya run tidak menghabiskan memori; rentang maksimal 31 hari.

**Aturan pencocokan.** Kunci pencocokan adalah reference KeiPay (`payments.external_id`: `kp_pay_…`, atau `{slug}:{order_id}` untuk payment lama), dibatasi ke connection run itu. Baris PG dikelompokkan per reference (amount dan fee dijumlah dari transaksi yang sukses; `transaction_count` dicatat, jadi pembayaran ganda terlihat sebagai `amount_mismatch`). Hasil per reference, aturan pertama yang cocok menang:

1. Hanya PG, dan PG sukses → `missing_in_keipay`. Hanya PG tapi tidak sukses → bukan item.
2. Hanya KeiPay (sukses) → `missing_in_gateway`.
3. Currency atau amount beda → `amount_mismatch`.
4. Hanya satu sisi yang sukses (`PAID` dan `SETTLED` dianggap sama) → `status_mismatch`.
5. Keduanya sukses, kedua fee diketahui, dan berbeda → `fee_mismatch`.
6. Selain itu → `matched`.

Baris KeiPay = report dengan `connection_id` run dan `paid_at` di jendela yang dilebarkan, ditambah payment connection itu yang reference-nya muncul di baris PG (lookup per batch 1.000 reference, bukan per baris). Jendela baca dilebarkan 1 jam ke dua sisi, tetapi item hanya masuk run kalau waktu jangkarnya ada di `[from, to)`: `paid_at` KeiPay kalau KeiPay mencatatnya sukses, selain itu waktu transaksi PG paling awal. Dengan begitu payment yang dibayar 23:59:59 dan dibukukan PG beberapa detik setelah tengah malam tidak muncul sebagai `missing` di dua hari yang bersebelahan.

**Run (`reconciliation_runs`, `rec_`) dan item (`reconciliation_items`, `rci_`).** Run menyimpan connection, `range_start`/`range_end` (UTC, half-open), `status` (`queued` | `running` | `succeeded` | `failed`), `run_count`, `summary` JSONB (jumlah per hasil), `unresolved_count`, `error_code` + `error_message` (pesan tetap, teks PG hanya ke log), `created_by` (user), `started_at`, `finished_at`. Item menyimpan `reference`, `result`, `payment_id`, snapshot JSONB `keipay` dan `gateway` (nilai saat dihitung, bukan nilai live), `anchor_at`, dan resolusi (`resolved_at`, `resolved_by`, `resolution_note`). `UNIQUE (run_id, reference)`. Di snapshot `gateway`, `status` adalah status KeiPay hasil pemetaan dan bernilai `null` kalau status PG tidak dikenal (nilai mentahnya tetap di `provider_status`; kolom `gateway_status` di CSV kosong).

- **Job asynq.** Task `reconciliation:run` di queue `reconciliation`, yang dilayani server asynq sendiri dengan konkurensi 2 (`worker.Pool`); forward webhook punya server sendiri dengan konkurensi 10, jadi run yang panjang tidak pernah memakan worker webhook dan run lain menunggu di queue-nya. Task ID `reconciliation:run:{run_id}:{run_count}` (satu task per percobaan), timeout 60 menit, tanpa retry asynq: setiap kegagalan membuat run `failed` dengan kode error, dan admin menjalankannya ulang. Worker mengklaim run (`queued` → `running` dengan guard `run_count`), menarik baris PG, mencocokkan, lalu dalam satu transaksi yang mengunci baris run dan memeriksa ulang `run_count`: upsert item per `(run_id, reference)`, hapus item yang tidak ditemukan lagi, isi summary, `succeeded`. Selama menarik halaman PG, worker memperbarui `updated_at` run paling sering sekali per menit (dengan guard `run_count` dan status `running`), jadi run panjang yang masih berjalan tidak dianggap macet; kalau guard itu gagal karena run sudah di-run ulang, worker berhenti tanpa menulis apa pun. Task basi (run sudah di-run ulang) juga tidak menulis apa pun.
- **Run ulang** memakai run yang sama (`run_count + 1`), jadi item tidak pernah ganda. Item yang hasilnya sama mempertahankan resolusinya; yang hasilnya berubah dibuka lagi. Run yang `queued`/`running` ditolak `409 run_in_progress`, kecuali tidak berubah selama 15 menit (worker mati atau enqueue hilang), supaya run tidak bisa macet selamanya. Enqueue yang gagal setelah commit membuat run `failed` dengan kode `not_queued`.
- **Resolve** hanya untuk item yang bukan `matched`, dengan catatan 1–1.000 karakter tanpa U+0000 (kolom `text` Postgres tidak bisa menyimpannya; rule `invalid_character`), dan ditolak selama run-nya `queued`/`running` supaya tidak berebut dengan job (`409 run_in_progress`), juga kalau run-nya `failed` (`409 run_not_succeeded`), karena item-nya sisa run count sebelumnya dan akan dihitung ulang oleh run ulang berikutnya. Dicatat di audit (`reconciliation_item.resolve`) di transaksi yang sama, dan `unresolved_count` run ikut turun.

**Ringkasan dan export.** `GET /admin/payment-reports/summary` mengelompokkan report per periode × app × connection × currency (currency tidak pernah dijumlah lintas kode). Periode `total`, `day`, atau `month`, dipotong di tengah malam Asia/Jakarta (`date_trunc` atas `paid_at AT TIME ZONE 'Asia/Jakarta'`); batas rentang 366 hari (92 untuk `day`). Report dengan fee tidak diketahui dihitung `fee_unknown_count` dan net-nya = gross. Export CSV (report dan item run) di-stream dari cursor Postgres langsung ke respons lewat `encoding/csv` dengan flush berkala, tanpa menampung baris di memori. Waktu di CSV RFC 3339 dengan offset `+07:00`; sel teks yang diawali `=`, `+`, `-`, `@`, tab, atau CR diberi awalan `'` (formula injection). Kalau query gagal setelah byte pertama terkirim, handler membatalkan respons (`http.ErrAbortHandler`) supaya unduhan terlihat terpotong, bukan file pendek yang tampak lengkap.

**API dan otorisasi.** `GET /admin/payment-reports` (cursor `(paid_at, pay_)`), `/summary`, `/export`; `GET/POST /admin/reconciliation-runs`, `GET /admin/reconciliation-runs/{id}`, `POST …/rerun`, `GET …/items` (cursor, filter `result`, `resolved`), `GET …/items/export`, `GET /admin/reconciliation-items/{id}` (supaya deep link `?item=rci_…` terbuka langsung tanpa bergantung pada filter atau halaman list), `POST /admin/reconciliation-items/{id}/resolve`. Membaca memakai `payments:read`; permission `reconciliation:write` (owner, admin) untuk membuat run, run ulang, dan resolve. Error: `409 run_in_progress`, `409 run_not_succeeded`. Audit: `reconciliation_run.create`, `reconciliation_run.rerun`, `reconciliation_item.resolve`.

**Konsol.** Halaman Reconciliation punya tab Summary (rentang tanggal WIB, interval, filter chip app/connection, tabel ringkasan, daftar report dengan cursor, export CSV) dan Runs (daftar run, dialog jalankan rekonsiliasi). Detail run di `/reconciliation/runs/:id`: header status dan fakta kunci, kartu jumlah per hasil, tabel item dengan filter hasil dan resolved, panel item (dua sisi berdampingan, resolve dengan catatan), run ulang, export CSV. Halaman mengikuti mode Test/Live lewat filter `environment`. Tanggal dipilih dalam WIB dan dikirim sebagai instant `+07:00` (`to` = tengah malam hari berikutnya, eksklusif).

### Penyajian konsol

- Di production, server Go menyajikan build konsol (`handler.SPA`) untuk `GET`/`HEAD` yang tidak cocok dengan route API. File yang ada disajikan langsung; path lain mendapat `index.html` supaya reload dan deep link berfungsi.
- Path API yang tidak cocok mendapat `404 not_found` dalam envelope JSON, bukan `index.html`: `/v1`, `/admin`, `/healthz` beserta semua path di bawahnya, dan semua path di bawah `/webhooks/`. Method selain `GET`/`HEAD` di path konsol juga mendapat envelope JSON.
- `/webhooks` persis (tanpa segmen berikutnya) adalah halaman Webhooks di konsol, jadi `GET /webhooks` menyajikan `index.html`. Ini aman karena callback gateway selalu di bawah `/webhooks/<gateway>` (`/webhooks/{provider}/{connection_id}` dan alias `/webhooks/xendit`), dan aturannya berbasis path, bukan header `Accept`, supaya respons tidak bergantung pada klien. Route konsol baru tidak boleh memakai `/v1`, `/admin`, `/healthz`, atau path di bawah `/webhooks/`.
- Di dev, proxy Vite meneruskan prefix yang sama ke backend dan melewatkan navigasi browser (`GET` dengan `Accept: text/html`) ke SPA.

### Uang dan waktu

- Uang: `int64` dalam minor unit + kode ISO 4217. Untuk IDR, minor unit yang dipakai KeiPay adalah rupiah utuh (eksponen 0), sesuai gateway; tampilan `Rp150.000`. Gateway memakai major unit, jadi adapter mengonversi di batasnya menurut eksponen ISO 4217: Xendit menerima `amount` dan harga item invoice dalam major unit (USD 1050 sen dikirim `10.5`), dan amount di callback dibaca sebagai angka desimal persis lalu dikonversi ke minor unit. Invoice tanpa mata uang dianggap IDR, default Xendit.
- Waktu disimpan `timestamptz` UTC dan dikirim RFC 3339; konsol menampilkan Asia/Jakarta (WIB).

### Port lokal dan gerbang kualitas

- Port bisa diatur lewat `.env` (`HTTP_PORT`, `UI_PORT`, `POSTGRES_PORT`, `REDIS_PORT`) atau per perintah (`make dev API_PORT=18080 UI_PORT=15173`). Vite membaca target proxy dari `VITE_API_TARGET`.
- `make ci` (`scripts/ci.sh`) adalah gerbang sebelum merge: gofmt, `go vet`, lint spec, cek hasil generate, migrasi up/down/up, `go test` dengan Postgres dan Redis asli, typecheck, test, dan build frontend, lalu smoke e2e Playwright terhadap server yang menyajikan build konsol. Postgres dan Redis jalan di project compose `keipay-ci` pada port 25432/26379 (API 28080) supaya tidak bentrok dengan stack dev, dan selalu dibongkar di akhir, juga saat gagal. CI eksternal cukup memanggil target yang sama.

## Correctness Properties

Disusun supaya bisa dijadikan property-based atau table test.

1. **Routing hanya memilih kandidat yang layak.** Untuk input dan snapshot health apa pun, setiap connection hasil `Plan` aktif, milik merchant app, mendukung currency, tidak OPEN, tidak maintenance, dan amount-nya dalam batas.
2. **Isolasi merchant.** `Plan` tidak pernah mengembalikan connection milik merchant lain, termasuk saat `connection` diminta eksplisit.
3. **Explicit tanpa fallback.** Kalau `provider` atau `connection` diisi dan `allow_explicit_fallback = false`, `Plan` mengembalikan paling banyak 1 kandidat.
4. **Tidak ada fallback setelah `ErrRejected`.** Attempt loop berhenti pada attempt pertama yang ditolak.
5. **Tidak ada tagihan kedua tanpa kepastian.** Setelah `ErrUnknownOutcome`, attempt berikutnya hanya dibuat kalau `FindByReference` mengembalikan `ErrNotFound`.
6. **Paling banyak satu attempt aktif per payment.**
7. **Distribusi weighted.** Dengan RNG yang di-seed, frekuensi pilihan pertama dari N = 10.000 draw berada dalam ±3% dari proporsi bobot.
8. **Pemetaan status total.** Setiap status yang terdokumentasi di setiap adapter terpetakan ke `domain.PaymentStatus`, dan status yang tidak dikenal tidak pernah dipetakan ke nilai default (dicatat `unmapped`).
9. **Verifikasi per connection.** Webhook yang ditandatangani dengan secret connection A dan dikirim ke URL connection B selalu ditolak.
10. **Idempotency inbound.** Memproses event dengan `(connection_id, event_key, provider_status)` yang sama dua kali menghasilkan state dan jumlah delivery yang sama seperti memprosesnya sekali.
11. **Poll dan webhook ekuivalen.** Event dengan `Source = "poll"` dan `Source = "webhook"` yang isinya sama menghasilkan perubahan state yang sama.
12. **Kredensial tidak bocor.** Respons admin, log, dan audit tidak pernah memuat nilai field `Secret: true`.
13. **Round-trip enkripsi kredensial**, termasuk setelah master key dirotasi.

## Testing Strategy

- **Service (unit):** `providertest.Fake` yang bisa diprogram untuk mengembalikan OK, `ErrRejected`, `ErrRetryable`, atau `ErrUnknownOutcome` per call. Mencakup attempt loop, adopsi, reconciler, dan properti 1–7, 10, 11.
- **Adapter (unit):** `httptest.Server` dengan fixture request dan respons asli dari dokumentasi atau sandbox masing-masing PG, plus fixture webhook dengan signature valid dan tidak valid (properti 8, 9).
- **Repository (integration):** Postgres asli, mengikuti pola `webhook_tx_integration_test.go` yang sudah ada, terutama untuk lock `GetAttemptByReferenceForUpdate` dan unique constraint.
- **Sandbox (manual/CI opsional):** satu skenario end-to-end per PG dengan kredensial sandbox dari env CI. Dilewati kalau env tidak tersedia.
- **Regresi:** test perilaku yang sudah ada (misalnya `webhook_service_test.go`, `payment_service_test.go`, `worker/sweeper_test.go`) harus tetap lulus pada setiap perubahan, dan hanya diubah kalau perilakunya memang sengaja diubah. Setiap bug fix membawa test regresi yang gagal sebelum fix.

## Roadmap

Setiap fase bisa dirilis dan dijalankan sendiri. Fondasi sampai rekonsiliasi membuat KeiPay lengkap untuk satu gateway dengan konsol yang utuh; routing multi-gateway dan adapter berikutnya menambah multi-gateway. Perubahan yang menyentuh app yang sudah ada dilakukan expand/contract, sehingga integrasi yang sudah berjalan tetap jalan selama masa transisi.

| Fase | Isi | Butuh | Selesai kalau |
|---|---|---|---|
| 0. Fondasi | Konvensi backend (ID berprefix, error envelope, cursor pagination, audit log), kontrak `api/openapi.yaml` + `make gen`, fondasi frontend (stack, struktur per fitur, `DESIGN.md`, komponen bersama, app shell), port lokal yang bisa diatur, gerbang `make ci` | – | `make ci` lulus; konsol lama tetap berfungsi, rapi di mobile |
| 1. Auth dan user | `admin_users` → `users` (`usr_`, role owner/admin/viewer), session cookie di Redis, CSRF, first-run setup, rate limit login, matriks otorisasi per role | 0 | Matriks role × endpoint teruji |
| 2. Apps dan identifier | `tenants` → `apps` (`app_`, environment test/live), `app_api_keys` dengan rotasi tumpang tindih, payment `pay_` + metadata + `Idempotency-Key`, reference `kp_<pay id>` lewat tabel dengan fallback `external_id` lama, `/admin/apps` | 1 | Lookup legacy dan key per environment teruji |
| 3. Connections | `internal/provider` (port, registry, kelas error), Xendit dipindah ke `internal/provider/xendit`, `merchants`, `connections` (`con_`), `connection_channels`, migrasi env Xendit menjadi connection, webhook per connection dengan alias `/webhooks/xendit`, form dari schema | 2 | Verifikasi webhook per connection dan alias lama berfungsi |
| 4. Webhook router | Katalog event, `webhook_endpoints` (`whe_`) per app, `events` (`evt_`) + delivery fan-out, envelope v2 dan `X-KeiPay-Signature`, header lama untuk endpoint hasil migrasi, auto-disable, `inbound_webhooks` | 2, 3 | Fan-out, signature v1, dan replay teruji |
| 5. Rekonsiliasi | `ledger_entries` → `payment_reports`, matcher independen sumber, `reconciliation_runs` (`rec_`) + item sebagai job asynq, resolve dengan audit, export CSV streaming | 3, 4 | Setiap jenis hasil match teruji |
| 6. Routing multi-gateway | `app_connections`, konfigurasi routing berversi, `payment_attempts`, attempt loop + adopsi, `RoutingService`, reconciler, simulate, timeline payment | 3, 4 | Properti 1–7, 10, 11 lulus |
| 7. Adapter Midtrans | Snap + webhook + status + cancel, tanpa perubahan frontend | 6 | Skenario sandbox end-to-end lulus |
| 8. Adapter DOKU | Jalur Checkout atau SNAP BI (belum dipilih; memengaruhi skema signature dan kredensial), tanpa perubahan frontend | 6 | Skenario sandbox end-to-end lulus |
| 9. Health dan dashboard | Circuit breaker Redis, maintenance manual/terjadwal, `/admin/health`, dashboard | 6 | Transisi breaker dan fail-open teruji, diverifikasi di browser |

## Catatan hukum

- KeiPay tidak menampung atau menyalurkan dana, jadi deployment self-host oleh satu bisnis untuk aplikasinya sendiri tidak masuk wilayah izin PJP. Memakai satu connection untuk menerima pembayaran bisnis lain (pemilik berbeda) **bukan** pemakaian yang didukung: app yang melayani bisnis di luar pemilik akun PG harus memakai connection milik pemilik bisnis itu sendiri.
- Menjalankan KeiPay sebagai **layanan untuk merchant lain** berarti operatornya memegang API key dan data transaksi pihak ketiga. Uang tetap tidak lewat operator, tapi ada kewajiban UU PDP dan kemungkinan batasan ToS masing-masing PG soal pihak ketiga. Ini di luar cakupan v1 dan perlu kajian hukum sendiri.
- Dokumen ini bukan nasihat hukum.
