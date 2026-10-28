# Design Document: KeiPay

## Overview

KeiPay adalah versi open source dari KeiPayment: **payment orchestrator self-hosted** yang memberi banyak aplikasi satu API dan satu format webhook untuk beberapa payment gateway (Xendit, Midtrans, DOKU, dan seterusnya).

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

1. **KeiPay tidak pernah memegang dana.** Setiap transaksi dibuat dengan kredensial PG milik merchant yang memiliki aplikasi itu, sehingga uang langsung masuk ke akun PG merchant tersebut. KeiPay hanya meneruskan API call dan webhook. Model "satu saldo bersama lalu settlement manual" milik KeiPayment **dihapus**.
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
- Refund, payout atau disbursement (fase berikutnya).
- Bobot adaptif dari success rate dan strategi `cheapest` (v2).
- Mode SaaS multi-merchant publik (lihat [Catatan hukum](#catatan-hukum)).

## Istilah

| Istilah | Arti |
|---|---|
| **Merchant** | Pemilik bisnis dan pemilik akun PG. Self-host untuk satu bisnis berarti satu merchant default. |
| **Connection** | Satu set kredensial ke satu akun PG milik merchant, misalnya `xendit-prod` atau `midtrans-sandbox`. |
| **Channel** | Metode pembayaran yang dinormalisasi: `QRIS`, `VA_BCA`, `EWALLET_OVO`, `CARD`, dan seterusnya. |
| **App** | Aplikasi klien yang memanggil KeiPay. Pengganti `tenant` di KeiPayment. Punya `api_key`/`api_secret` dan `webhook_url`. |
| **Payment** | Satu permintaan pembayaran dari app, unik per `(app_id, order_id)`. |
| **Attempt** | Satu percobaan membuat checkout di satu connection. Satu payment bisa punya beberapa attempt karena fallback, tapi paling banyak **satu** yang aktif. |

## Architecture

KeiPay tetap memakai arsitektur ports-and-adapters milik KeiPayment. Perubahannya: port `xendit.Client` digeneralisasi menjadi port `provider.Provider`, dengan satu adapter per PG.

### Posisi komponen

```
cmd/server/main.go            manual DI; mendaftarkan adapter ke provider.Registry
internal/
  domain/
    merchant.go               + Merchant (BARU)
    connection.go             + Connection, ConnectionChannel, Channel enum (BARU)
    app.go                    ~ Tenant → App, + MerchantID, RoutingConfig
    payment.go                ~ + Provider, ConnectionID, ProviderRef, Methods; - XenditInvoiceID
    attempt.go                + PaymentAttempt, AttemptStatus (BARU)
    webhook.go                ~ WebhookEvent → InboundWebhook (per connection, raw payload)
    routing.go                + RoutingDecision, Strategy (BARU)
    repository.go             + Merchant/Connection/Attempt repos
  provider/
    provider.go               + port Provider, Capabilities, error classes, Registry (BARU)
    xendit/                   ~ pindahan internal/xendit, implementasi Provider
    midtrans/                 + adapter Snap (BARU)
    doku/                     + adapter Checkout (BARU)
    providertest/             + fake Provider untuk test service
  service/
    payment_service.go        ~ Create via RoutingService + attempt loop
    routing_service.go        + seleksi kandidat dan strategi (BARU, logic murni)
    webhook_service.go        ~ Process(InboundEvent) generik, lookup via attempt reference
    connection_service.go     + CRUD, test credentials, enkripsi kredensial (BARU)
    health_service.go         + circuit breaker per (connection, channel) (BARU)
    reconcile_service.go      + poll status PG untuk attempt PENDING (BARU)
  repository/
    postgres/                 + merchant_repo, connection_repo, attempt_repo
    redis/                    + health.go (counter sliding window, state breaker)
  transport/http/
    router.go                 ~ /webhooks/:provider/:connection_public_id
    handler/webhook_handler.go~ generik: resolve connection → adapter.Verify → Parse
  worker/
    reconciler.go             + task periodik reconcile (BARU)
    forwarder.go, sweeper.go  tetap
```

### Kenapa port ada di `internal/provider`, bukan di `domain`

Port ini mirip `internal/xendit` yang sekarang: tipe input dan output-nya spesifik untuk integrasi PG (credential schema, raw payload, klasifikasi error), bukan konsep bisnis. Service bergantung pada `provider.Registry`, adapter mengimpor `provider` dan `domain`, dan `domain` tetap tanpa dependensi.

## Alur

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

Langkah 3 dan 4a di-commit **sebelum** memanggil PG, supaya crash di tengah jalan selalu meninggalkan jejak yang bisa direkonsiliasi. Ini menggantikan pola "invoice yatim di-expire" di `PaymentService.Create` sekarang.

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

Routing ke app dilakukan **lewat lookup attempt**, bukan parsing `external_id`. Slug app tidak perlu lagi dibocorkan ke PG.

### 3. Forward ke app

Tetap memakai mekanisme KeiPayment: baris `webhook_deliveries`, task asynq dengan ID dari delivery id, retry sampai `WEBHOOK_MAX_RETRY`, `Sweeper` untuk enqueue yang gagal, dan replay dari admin. Yang berubah hanya payload (v2) dan skema signature (lihat [Webhook keluar](#webhook-keluar-v2)).

### 4. Rekonsiliasi status

`worker.Reconciler` berjalan periodik (default 5 menit):

- Attempt `CREATED` dengan payment `PENDING` yang lebih tua dari `RECONCILE_MIN_AGE` (default 15 menit) dan belum kedaluwarsa → `adapter.GetStatus`. Kalau statusnya maju, dimasukkan lewat `WebhookService.Process` yang sama sebagai `InboundEvent{Source: "poll"}`, sehingga idempotency dan state machine-nya sama persis dengan webhook.
- Attempt `UNKNOWN` → `FindByReference`. Kalau ditemukan dan payment belum punya attempt aktif, attempt itu diadopsi. Kalau sudah ada attempt aktif lain, attempt yang ditemukan dibatalkan lewat `CancelCheckout`.
- Batas rate per connection supaya tidak memicu rate limit PG.

## Components and Interfaces

### Port `provider.Provider`

```go
package provider

type Name string // "xendit", "midtrans", "doku"

type Credentials map[string]string // hasil dekripsi; key sesuai CredentialSchema

type CredentialField struct {
	Key    string // "secret_key", "server_key", "client_id", ...
	Label  string
	Secret bool   // disamarkan di UI dan log
}

type Capabilities struct {
	Channels       []domain.Channel
	Currencies     []string
	CanRestrictMethods bool // hosted checkout bisa dibatasi ke metode tertentu
	CanCancel      bool
	ReportsFee     bool     // webhook membawa fee aktual
}

type CheckoutInput struct {
	Reference          string // deterministik, unik per connection; dipakai sebagai order/external id di PG
	Amount             int64
	Currency           string
	Description        string
	Methods            []domain.Channel // kosong = semua yang aktif di connection
	Items              []domain.OrderItem
	Customer           *domain.Customer
	SuccessRedirectURL string
	FailureRedirectURL string
	ExpiresIn          time.Duration
}

type CheckoutOutput struct {
	ProviderPaymentID string
	CheckoutURL       string
	ExpiresAt         *time.Time
	Raw               []byte
}

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

type InboundEvent struct {
	EventKey          string // kunci dedupe bersama ProviderStatus; default = ProviderPaymentID
	Reference         string
	ProviderPaymentID string
	Status            domain.PaymentStatus
	ProviderStatus    string // status asli PG, untuk audit
	Channel           domain.Channel
	Amount            int64
	Currency          string
	Fee               *int64
	PaidAt            *time.Time
	Source            string // "webhook" | "poll"
}

type Provider interface {
	Name() Name
	CredentialSchema() []CredentialField
	Capabilities() Capabilities

	TestCredentials(ctx context.Context, c Credentials) error
	CreateCheckout(ctx context.Context, c Credentials, in CheckoutInput) (*CheckoutOutput, error)
	FindByReference(ctx context.Context, c Credentials, reference string) (*StatusOutput, error) // ErrNotFound kalau tidak ada
	GetStatus(ctx context.Context, c Credentials, providerPaymentID string) (*StatusOutput, error)
	CancelCheckout(ctx context.Context, c Credentials, providerPaymentID string) error

	VerifyWebhook(c Credentials, h http.Header, rawBody []byte) error
	ParseWebhook(rawBody []byte) (*InboundEvent, error)
}
```

Adapter yang tidak memakai `net/http` di `VerifyWebhook` cukup membaca header. Port ini tidak mengimpor Gin.

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
type Registry struct{ providers map[Name]Provider }
func (r *Registry) Get(n Name) (Provider, error)
func (r *Registry) All() []Provider // untuk UI: daftar provider + CredentialSchema
```

Didaftarkan di `cmd/server/main.go`. Menambah PG baru berarti menulis satu paket adapter dan satu baris registrasi.

### Catatan per adapter

Detail ini **harus diverifikasi ulang terhadap dokumentasi resmi** saat adapter ditulis.

| | Xendit (Invoice) | Midtrans (Snap) | DOKU (Checkout) |
|---|---|---|---|
| Reference di PG | `external_id` | `order_id` (unik per merchant, maks 50 karakter) | `order.invoice_number` |
| Batasi metode | `payment_methods` | `enabled_payments` | `payment.payment_method_types` |
| Verifikasi webhook | header `x-callback-token` == token connection (constant-time) | `signature_key` = SHA512(`order_id`+`status_code`+`gross_amount`+server key) | header `Signature`: HMAC-SHA256 atas Client-Id, Request-Id, Timestamp, Target, Digest |
| Lookup by reference | `GET /v2/invoices?external_id=` | `GET /v2/{order_id}/status` | API status by invoice number |
| Batalkan | expire invoice | `POST /v2/{order_id}/cancel` atau expire | verifikasi saat implementasi |
| Kredensial | `secret_key`, `callback_token` | `server_key`, `client_key` (opsional) | `client_id`, `secret_key` |

Kode `internal/xendit` (`sdk_client.go`, `ExpireInvoice`) dipindah ke `internal/provider/xendit`, dan pemetaan status dari `webhook_service.go` pindah ke `ParseWebhook`.

### Normalisasi status

State machine `domain.PaymentStatus` tetap memakai `CanTransitionTo` yang sekarang (PENDING → PAID → SETTLED; sukses yang terlambat setelah EXPIRED/FAILED tetap di-apply). Setiap adapter memetakan status aslinya:

| Normal | Xendit | Midtrans `transaction_status` | DOKU |
|---|---|---|---|
| `PENDING` | `PENDING` | `pending`; `capture` + `fraud_status=challenge` | pending |
| `PAID` | `PAID` | `settlement`; `capture` + `fraud_status=accept` | success |
| `SETTLED` | `SETTLED` | tidak ada (Midtrans tidak mengirim event dana cair) | tidak ada |
| `EXPIRED` | `EXPIRED` | `expire` | expired |
| `FAILED` | – | `deny`, `cancel`, `failure` | failed |

Status yang tidak dikenal → `ParseWebhook` mengembalikan error, inbound dicatat dengan `result = unmapped`, PG tetap di-ack, dan admin mendapat alert. Webhook tidak boleh di-apply diam-diam.

`SETTLED` bersifat opsional. App tidak boleh bergantung pada event ini karena hanya sebagian PG yang mengirimnya, dan ini harus tertulis di dokumentasi API.

## Routing

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

Migrasi baru di `migrations/`. Tabel lama diubah lewat rename, tidak dibuat ulang.

```sql
CREATE TABLE merchants (
  id          BIGSERIAL PRIMARY KEY,
  name        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE connections (
  id               BIGSERIAL PRIMARY KEY,
  public_id        TEXT NOT NULL UNIQUE,          -- acak (mis. con_<26 char>), dipakai di URL webhook
  merchant_id      BIGINT NOT NULL REFERENCES merchants(id),
  provider         TEXT NOT NULL,                 -- xendit | midtrans | doku
  name             TEXT NOT NULL,                 -- xendit-prod
  environment      TEXT NOT NULL CHECK (environment IN ('sandbox','production')),
  credentials_enc  TEXT NOT NULL,                 -- JSON Credentials, envelope-encrypted
  key_version      INT  NOT NULL DEFAULT 1,
  active           BOOLEAN NOT NULL DEFAULT true,
  last_tested_at   TIMESTAMPTZ,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (merchant_id, name)
);

CREATE TABLE connection_channels (
  connection_id      BIGINT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
  channel            TEXT NOT NULL,               -- QRIS, VA_BCA, ...
  enabled            BOOLEAN NOT NULL DEFAULT true,
  min_amount         BIGINT,
  max_amount         BIGINT,
  fee_flat           BIGINT NOT NULL DEFAULT 0,   -- untuk strategi cheapest (v2)
  fee_bps            INT    NOT NULL DEFAULT 0,   -- basis point
  maintenance_from   TIMESTAMPTZ,
  maintenance_until  TIMESTAMPTZ,
  PRIMARY KEY (connection_id, channel)
);

ALTER TABLE tenants RENAME TO apps;
ALTER TABLE apps ADD COLUMN merchant_id BIGINT REFERENCES merchants(id);
ALTER TABLE apps ADD COLUMN routing_config JSONB NOT NULL DEFAULT '{}';
-- setelah backfill: SET NOT NULL pada merchant_id

CREATE TABLE app_connections (
  app_id         BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
  connection_id  BIGINT NOT NULL REFERENCES connections(id),
  priority       INT NOT NULL,
  weight         INT NOT NULL DEFAULT 1 CHECK (weight >= 0),
  enabled        BOOLEAN NOT NULL DEFAULT true,
  PRIMARY KEY (app_id, connection_id)
);
-- invariant merchant sama dicek di service (ConnectionService/AppService), bukan FK

ALTER TABLE payments RENAME COLUMN tenant_id TO app_id;
ALTER TABLE payments RENAME COLUMN invoice_url TO checkout_url;
ALTER TABLE payments ADD COLUMN provider TEXT;
ALTER TABLE payments ADD COLUMN connection_id BIGINT REFERENCES connections(id);
ALTER TABLE payments ADD COLUMN active_attempt_id BIGINT;
ALTER TABLE payments ADD COLUMN channel TEXT;                -- channel yang dipakai pembayar
ALTER TABLE payments ADD COLUMN methods TEXT[];              -- yang diminta app
ALTER TABLE payments ADD COLUMN routing_decision JSONB;
ALTER TABLE payments ADD COLUMN duplicate_paid BOOLEAN NOT NULL DEFAULT false;
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

ALTER TABLE webhook_events RENAME TO inbound_webhooks;
ALTER TABLE inbound_webhooks ADD COLUMN connection_id BIGINT REFERENCES connections(id);
ALTER TABLE inbound_webhooks ADD COLUMN provider TEXT;
ALTER TABLE inbound_webhooks RENAME COLUMN xendit_id TO event_key;
ALTER TABLE inbound_webhooks ADD COLUMN source TEXT NOT NULL DEFAULT 'webhook';
ALTER TABLE inbound_webhooks ADD COLUMN signature_valid BOOLEAN;
ALTER TABLE inbound_webhooks ADD COLUMN raw_payload JSONB;
ALTER TABLE inbound_webhooks ADD COLUMN result TEXT;        -- applied|stale|mismatch|duplicate|unmapped
-- UNIQUE (xendit_id, status) lama diganti UNIQUE (connection_id, event_key, status),
-- dengan status = status asli PG; semantik dedupe sama seperti sekarang, tapi per connection

ALTER TABLE webhook_deliveries RENAME COLUMN tenant_id TO app_id;
ALTER TABLE ledger_entries RENAME TO payment_reports;       -- laporan, bukan settlement
ALTER TABLE payment_reports RENAME COLUMN tenant_id TO app_id;
ALTER TABLE payment_reports RENAME COLUMN xendit_fee TO provider_fee;
ALTER TABLE payment_reports ADD COLUMN connection_id BIGINT REFERENCES connections(id);
-- UNIQUE (payment_id, type) tetap
```

Catatan terhadap skema yang ada (`000001_init.up.sql`):

- `payments.external_id` (`NOT NULL UNIQUE`, format `{slug}:{order_id}`) tidak lagi dipakai untuk routing. Kolom ini dibiarkan untuk data lama dan diisi `kp_<payment_public_id>` untuk payment baru, lalu `NOT NULL` dilepas di versi mayor berikutnya.
- `payments.xendit_invoice_id` dan index `idx_payments_xendit_invoice_id` di-backfill ke `payment_attempts` (satu attempt `CREATED` per payment lama, `reference = external_id`), lalu di-drop setelah adapter Xendit hanya membaca dari attempt.
- Index lama yang memakai `tenant_id` ikut ter-rename kolomnya. Nama index (`idx_payments_tenant_status`, `idx_deliveries_tenant_status`, `idx_ledger_tenant_created`) di-rename supaya konsisten.

### Penyimpanan kredensial

- **Envelope encryption.** Setiap connection punya data key acak (AES-256-GCM). Data key dienkripsi dengan master key dari env `ENCRYPTION_KEY` (yang sekarang dipakai untuk `api_secret`). `key_version` memungkinkan rotasi master key tanpa mengubah format.
- Kredensial **tidak pernah** dikembalikan oleh API admin setelah disimpan. Yang dikembalikan hanya 4 karakter terakhir per field `Secret`.
- Log dan audit memakai `CredentialSchema` untuk menyamarkan field rahasia.
- Dokumentasi menyarankan API key dengan permission minimal (misalnya Xendit restricted key: invoice read/write saja).

## API Surface

### `POST /v1/payments`

Header dan HMAC tetap sama seperti KeiPayment (`X-Api-Key`, `X-Timestamp`, `X-Signature`).

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

`invoice_url` adalah alias `checkout_url` untuk kompatibilitas app KeiPayment. Alias ini dihapus di versi mayor berikutnya.

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

Sama seperti sekarang, ditambah `provider`, `connection`, dan `channel`.

### Webhook keluar (v2)

```
POST {app.webhook_url}
X-KeiPay-Event-Id: evt_01J...
X-KeiPay-Timestamp: 1730109600
X-KeiPay-Signature: v1=<hex HMAC-SHA256("{timestamp}.{rawBody}", api_secret)>
```

```json
{
  "event": "payment.paid",
  "event_id": "evt_01J...",
  "payment_id": "pay_01J...",
  "order_id": "ord-123",
  "status": "PAID",
  "amount": 150000,
  "currency": "IDR",
  "provider": "midtrans",
  "connection": "midtrans-prod",
  "channel": "QRIS",
  "provider_fee": null,
  "paid_at": "2026-10-28T10:05:40Z",
  "occurred_at": "2026-10-28T10:05:40Z"
}
```

- Event: `payment.paid`, `payment.settled`, `payment.expired`, `payment.failed`, `payment.duplicate`.
- **Perubahan dari KeiPayment:** signature mencakup timestamp, sehingga app bisa menolak replay lama (toleransi yang disarankan 5 menit). Header lama `X-Kei-Signature` (body saja) tetap dikirim selama masa transisi, bisa dimatikan per app.
- Tetap at-least-once. App melakukan dedupe dengan `event_id`.

### Admin API (ringkas)

```
GET/POST        /admin/connections
GET/PUT/DELETE  /admin/connections/:id          (DELETE = nonaktifkan; ditolak kalau masih dipakai app)
POST            /admin/connections/:id/test
GET/PUT         /admin/connections/:id/channels
POST            /admin/connections/:id/maintenance
GET             /admin/providers                 (daftar adapter + CredentialSchema untuk form UI)

GET/POST        /admin/apps                      (pengganti /admin/tenants)
GET/PUT         /admin/apps/:id
POST            /admin/apps/:id/rotate
GET/PUT         /admin/apps/:id/routing
POST            /admin/apps/:id/routing/simulate (dry-run Plan, tidak memanggil PG)
POST            /admin/apps/:id/webhook/test     (kirim event uji ke webhook_url)

GET             /admin/payments                  (filter: app, provider, connection, status, waktu)
GET             /admin/payments/:id/timeline     (attempts + inbound + deliveries, terurut waktu)
GET             /admin/webhooks/inbound
GET             /admin/webhook-deliveries
POST            /admin/webhook-deliveries/:id/replay
GET             /admin/health                    (state breaker per connection/channel)
GET             /admin/reports                   (rekap per app × provider; export CSV)
```

Semua list memakai pagination. Endpoint yang mengubah state dicatat di `audit_logs`. Tabel ini belum ada migrasinya, jadi perlu dibuat di fase 2.

## Admin Console (frontend/)

Stack tetap: React, Tailwind, Phosphor icons, motion. **Sebelum layar baru dibuat**, tulis `DESIGN.md` berisi token warna, badge status (sehat, menurun, down, maintenance, circuit open), pola tabel, dan state kosong/loading/error.

| Menu | Isi |
|---|---|
| Dashboard | Volume per provider, success rate create, status health, delivery gagal |
| Apps | List → detail dengan tab Overview, API keys, Webhook, Routing, Logs |
| Connections | List + wizard tambah connection, health per channel, toggle maintenance |
| Payments | List + **timeline** per payment |
| Webhooks | Tab Inbound (dari PG, raw + hasil verifikasi) dan Outbound (ke app), replay |
| Reports | Rekap per app × provider, export CSV |
| Settings | User, role, audit log |

Layar kunci:

1. **Wizard tambah connection:** pilih provider → form dibentuk dari `CredentialSchema` → *Test connection* → tampilkan URL webhook connection itu beserta tombol copy dan petunjuk di menu mana URL dipasang di dashboard PG → aktifkan channel dan isi fee.
2. **Routing per app:** pilihan strategi, daftar connection yang bisa di-drag (urutan, bobot, health 24 jam, status per channel), pengaturan fallback, tombol *Simulasikan routing* (memanggil `/routing/simulate`).
3. **Timeline payment:** request app → keputusan routing (kandidat, skor, alasan penolakan) → setiap attempt → inbound webhook (raw, verifikasi) → normalisasi → setiap percobaan delivery.
4. **Onboarding saat pertama dijalankan:** tambah connection sandbox → buat app → copy kredensial → kirim payment uji → lihat webhook sampai.

Status selalu ditampilkan sebagai teks label, tidak hanya warna. Setiap layar dicek di lebar mobile.

## Correctness Properties

Disusun supaya bisa dijadikan property-based atau table test.

1. **Routing hanya memilih kandidat yang layak.** Untuk input dan snapshot health apa pun, setiap connection hasil `Plan` aktif, milik merchant app, mendukung currency, tidak OPEN, tidak maintenance, dan amount-nya dalam batas.
2. **Isolasi merchant.** `Plan` tidak pernah mengembalikan connection milik merchant lain, termasuk saat `connection` diminta eksplisit.
3. **Explicit tanpa fallback.** Kalau `provider` atau `connection` diisi dan `allow_explicit_fallback = false`, `Plan` mengembalikan paling banyak 1 kandidat.
4. **Tidak ada fallback setelah `ErrRejected`.** Attempt loop berhenti pada attempt pertama yang ditolak.
5. **Tidak ada tagihan kedua tanpa kepastian.** Setelah `ErrUnknownOutcome`, attempt berikutnya hanya dibuat kalau `FindByReference` mengembalikan `ErrNotFound`.
6. **Paling banyak satu attempt aktif per payment.**
7. **Distribusi weighted.** Dengan RNG yang di-seed, frekuensi pilihan pertama dari N = 10.000 draw berada dalam ±3% dari proporsi bobot.
8. **Pemetaan status total.** Setiap status yang terdokumentasi di setiap adapter terpetakan ke `domain.PaymentStatus`, dan status yang tidak dikenal menghasilkan error, bukan nilai default.
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
- **Regresi:** semua test KeiPayment yang ada (`webhook_service_test.go`, `payment_service_test.go`, `sweeper_test.go`) tetap harus lulus setelah fase refactor, dan baru diubah setelah perilakunya memang sengaja diubah.

## Rencana migrasi dari KeiPayment

Setiap fase bisa di-merge dan dijalankan sendiri. Fase 1–2 tidak boleh mengubah perilaku untuk app yang sudah ada.

| Fase | Isi | Selesai kalau |
|---|---|---|
| 0. Persiapan OSS | Repo baru tanpa history (`keipay`) berbentuk monorepo `backend/` + `frontend/`, lisensi MIT, README, `SECURITY.md`, `CONTRIBUTING.md`; buang seed tenant internal dan contoh dengan nama produk internal; satu image Docker yang menyajikan API dan admin console | Repo publik bisa `make up && make seed && make dev`, dan `docker compose up` menjalankan semuanya |
| 1. Port provider | `internal/provider` + adapter Xendit hasil pindahan `internal/xendit`; `PaymentService`/`WebhookService` memakai port | Semua test lama lulus tanpa perubahan perilaku |
| 2. Model data | `merchants`, `connections`, `apps`, `app_connections`; migrasi mengubah env `XENDIT_SECRET_KEY`/`XENDIT_CALLBACK_TOKEN` menjadi satu connection milik merchant default; route `/webhooks/:provider/:public_id`; `/webhooks/xendit` tetap ada sebagai alias ke connection default | Webhook lama dan baru sama-sama berfungsi |
| 3. Attempt dan routing dasar | `payment_attempts`, `RoutingService` (explicit, priority, weighted statis), attempt loop + adopsi, reconciler | Properti 1–7, 10, 11 lulus |
| 4. Adapter Midtrans | Snap + webhook + status + cancel | Skenario sandbox end-to-end lulus |
| 5. Adapter DOKU | Checkout + webhook + status | Skenario sandbox end-to-end lulus |
| 6. Health dan UI | Breaker Redis, maintenance, halaman Connections/Routing/Timeline, onboarding | Diverifikasi di browser, termasuk lebar mobile |
| 7. v2 | Bobot adaptif, `cheapest`, refund lewat port | Spec terpisah |

Di fase 2, kalau ada app yang memakai KeiPayment untuk bisnis di luar pemilik akun Xendit, app itu harus dipindah ke connection milik pemiliknya sendiri. Jangan terus memakai connection default.

## Catatan hukum

- KeiPay tidak menampung atau menyalurkan dana, jadi deployment self-host oleh satu bisnis untuk aplikasinya sendiri tidak masuk wilayah izin PJP. README harus menyatakan dengan tegas bahwa memakai satu connection untuk menerima pembayaran bisnis lain (pemilik berbeda) **bukan** pemakaian yang didukung.
- Menjalankan KeiPay sebagai **layanan untuk merchant lain** berarti operatornya memegang API key dan data transaksi pihak ketiga. Uang tetap tidak lewat operator, tapi ada kewajiban UU PDP dan kemungkinan batasan ToS masing-masing PG soal pihak ketiga. Ini di luar cakupan v1 dan perlu kajian hukum sendiri.
- Dokumen ini bukan nasihat hukum.

## Open Questions

1. ~~Nama repo dan lisensi~~ Diputuskan: `keipay`, MIT.
2. DOKU: integrasi Checkout biasa atau jalur SNAP BI? Ini memengaruhi skema signature dan kredensial.
3. Apakah `payment_id` publik perlu diganti ke format ULID (`pay_...`) sekarang? Ini memengaruhi `reference` dan URL, dan lebih murah dilakukan di fase 2.
4. Perlu dukungan multi-currency di v1, atau IDR saja?
5. Batas waktu masa transisi header `X-Kei-Signature` lama.
