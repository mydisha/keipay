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
| **App** | Aplikasi klien yang memanggil KeiPay. Pengganti `tenant` di KeiPayment. Punya satu environment (`test` atau `live`), banyak API key (`app_api_keys`, masing-masing dengan secret HMAC sendiri), satu webhook signing secret, dan `webhook_url`. |
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

Sejak fase 4: satu `events` per perubahan status, fan-out menjadi satu `webhook_deliveries` per endpoint app yang subscription-nya cocok, retry per endpoint yang dijadwalkan worker sendiri, `Sweeper` untuk delivery yang terlambat, dan replay dari admin. Detailnya di [Webhook router](#webhook-router).

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

-- apps dibuat di fase 2 sebagai tabel baru (lihat "Apps, API key, dan identifier");
-- tenants tidak di-rename.
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

-- payments.app_id ditambahkan di fase 2; tenant_id di-drop di migrasi contract.
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

-- webhook_deliveries.app_id dan ledger_entries.app_id ditambahkan di fase 2.
ALTER TABLE ledger_entries RENAME TO payment_reports;       -- laporan, bukan settlement
ALTER TABLE payment_reports RENAME COLUMN xendit_fee TO provider_fee;
ALTER TABLE payment_reports ADD COLUMN connection_id BIGINT REFERENCES connections(id);
-- UNIQUE (payment_id, type) tetap
```

Catatan terhadap skema yang ada (`000001_init.up.sql`):

- `payments.external_id` (`NOT NULL UNIQUE`, format `{slug}:{order_id}`) tidak lagi dipakai untuk routing. Kolom ini dibiarkan untuk data lama dan diisi `kp_<payment_public_id>` untuk payment baru, lalu `NOT NULL` dilepas di versi mayor berikutnya.
- `payments.xendit_invoice_id` dan index `idx_payments_xendit_invoice_id` di-backfill ke `payment_attempts` (satu attempt `CREATED` per payment lama, `reference = external_id`), lalu di-drop setelah adapter Xendit hanya membaca dari attempt.
- Index lama yang memakai `tenant_id` dibiarkan sampai migrasi contract; fase 2 menambah index padanannya di `app_id`.

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

Sketsa awal. Bentuk yang dibangun di fase 4 (envelope `{id, type, api_version, created_at, data}`, endpoint per app, header `X-KeiPay-*`) ada di [Webhook router](#webhook-router).

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

Sketsa awal; path webhook yang dibangun di fase 4 ada di [Webhook router](#webhook-router) dan `api/openapi.yaml`.

```
GET/POST        /admin/connections
GET/PUT/DELETE  /admin/connections/:id          (DELETE = nonaktifkan; ditolak kalau masih dipakai app)
POST            /admin/connections/:id/test
GET/PUT         /admin/connections/:id/channels
POST            /admin/connections/:id/maintenance
GET             /admin/providers                 (daftar adapter + CredentialSchema untuk form UI)

GET/POST        /admin/apps                      (pengganti /admin/tenants, fase 2)
GET/PATCH       /admin/apps/:id
POST            /admin/apps/:id/disable | /enable
GET/POST        /admin/apps/:id/keys
POST            /admin/apps/:id/keys/:key_id/rotate | /revoke
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

Semua list baru memakai cursor pagination (lihat [Konvensi](#konvensi)). Setiap endpoint admin yang mengubah state dicatat di `audit_logs` (tabelnya dibuat di fase 0).

## Admin Console (frontend/)

Stack (diputuskan di fase 0): React 19 + Vite + TypeScript sebagai SPA yang disajikan backend Go; Tailwind v4 dengan shadcn/ui (style new-york, Radix, CSS variables untuk light dan dark); ikon lucide; font IBM Plex Sans dan IBM Plex Mono; react-router 7; TanStack Query untuk server state dan TanStack Table untuk tabel; react-hook-form + zod; sonner; cmdk; openapi-typescript + openapi-fetch untuk client dari kontrak. Test memakai Vitest + Testing Library dan Playwright (headless). Phosphor dan motion dihapus. Library UI atau state management lain hanya ditambah dengan alasan yang dicatat di dokumen ini.

Token, komponen dasar, pola tabel, pola halaman detail, aturan state loading/empty/error, mobile, dark mode, dan format Rp/tanggal/ID ada di `DESIGN.md` di root repo. Badge health (sehat, menurun, down, maintenance, circuit open) ditambahkan ke sana di fase 9.

Struktur kode: `src/app` (router, providers, layout), `src/features/<fitur>/{api,components,pages,schemas}`, `src/shared/{ui,lib,api}`. Tipe API di `src/shared/api/schema.d.ts` di-generate dari `api/openapi.yaml` (`make gen`) dan tidak diedit tangan.

Navigasi final (sidebar yang bisa diciutkan, top bar dengan switch Test/Live dan banner amber saat Test, command palette ⌘K, toggle tema):

| Menu | Isi akhir | Fase 0 |
|---|---|---|
| Dashboard | Volume per provider, success rate create, status health, delivery gagal, item rekonsiliasi belum resolved | Empty state "segera hadir" |
| Apps | List → detail dengan tab Overview, API keys, Webhooks, Routing | Halaman tenant lama dipindah ke sini; fase 2: list cursor + detail dengan tab (Webhooks dan Routing masih empty state) |
| Connections | List + wizard tambah connection, health per channel, toggle maintenance | Empty state "segera hadir"; fase 3: list, wizard, detail (setting, kredensial, channel, test, aktif/nonaktif); health dan maintenance di fase 9 |
| Webhooks | Tab Outbound (delivery ke app, replay) dan Inbound (dari PG, raw + hasil verifikasi) | Halaman delivery lama (Outbound); fase 4: Outbound dan Inbound dengan cursor, filter, dan detail (log attempt, raw body); endpoint diatur di tab Webhooks detail app |
| Payments | List + **timeline** per payment | Halaman payment lama |
| Reconciliation | Run rekonsiliasi per connection, ringkasan per app × connection × periode, export CSV | Halaman ledger lama (ringkasan per tenant) |
| Settings | User, role, audit log | Audit log (read-only) dan ganti password |

Layar kunci:

1. **Wizard tambah connection:** pilih provider → form dibentuk dari `CredentialSchema` → *Test connection* → tampilkan URL webhook connection itu beserta tombol copy dan petunjuk di menu mana URL dipasang di dashboard PG → aktifkan channel dan isi fee.
2. **Routing per app:** pilihan strategi, daftar connection yang bisa di-drag (urutan, bobot, health 24 jam, status per channel), pengaturan fallback, tombol *Simulasikan routing* (memanggil `/routing/simulate`).
3. **Timeline payment:** request app → keputusan routing (kandidat, skor, alasan penolakan) → setiap attempt → inbound webhook (raw, verifikasi) → normalisasi → setiap percobaan delivery.
4. **Onboarding saat pertama dijalankan:** tambah connection sandbox → buat app → copy kredensial → kirim payment uji → lihat webhook sampai.

Status selalu ditampilkan sebagai teks label, tidak hanya warna. Setiap layar dicek di lebar mobile.

## Konvensi

Diputuskan di fase 0 dan berlaku untuk semua fase berikutnya.

### Kontrak HTTP

- `api/openapi.yaml` (OpenAPI 3.1) adalah sumber kebenaran kontrak. Setiap perubahan endpoint mengubah spec di commit yang sama. Spec di-lint dengan Redocly (`make lint-api`, aturan di `api/redocly.yaml`).
- Tipe TypeScript di-generate dengan openapi-typescript ke `frontend/src/shared/api/schema.d.ts` (`make gen`) dan dipakai lewat openapi-fetch. Hasil generate di-commit; gerbang `make ci` gagal kalau hasilnya tidak sinkron dengan spec.
- Webhook keluar ke app didokumentasikan di bagian `webhooks` spec.

### ID publik

- Format `<prefix>_<ULID>`, misalnya `pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C`: ULID 26 karakter Crockford base32 huruf besar, sehingga ID terurut menurut waktu pembuatan. Parse menerima huruf kecil dan menormalkannya.
- Prefix: `app_`, `pay_`, `con_`, `whe_`, `evt_`, `dlv_` (delivery webhook, fase 4), `inb_` (webhook masuk, fase 4), `usr_`, `key_`, `rec_`, `aud_`. Paket `internal/pkg/id` membuat, mem-parse, dan memvalidasi ID; prefix yang salah ditolak.
- Kolom `public_id TEXT NOT NULL UNIQUE` disimpan di samping `id BIGSERIAL` internal. ID bigint tidak pernah keluar lewat API baru.
- Pengecualian sementara: id bigint ledger entry (`/admin/ledger`) tetap keluar sampai entitasnya dipindah di fase 5. Delivery memakai `dlv_` sejak fase 4. Sejak fase 2, app (`app_`), API key (`key_`), dan payment (`pay_`) memakai ID publik di semua respons admin, termasuk referensi app/payment di baris ledger dan delivery. Satu-satunya bigint app/payment yang masih keluar adalah `payment_id` di `/v1` dan di payload webhook v1, demi kompatibilitas integrasi lama; `GET /v1/payments/{id}` menerima `pay_…` maupun integer lama.

### Error

- Semua error, di semua surface, berbentuk `{"error":{"code":"...","message":"...","details":{}}}`. `details` selalu objek.
- `code` stabil dan didefinisikan sebagai konstanta di `internal/domain`; `response.Error` memetakan error domain ke status HTTP dan code. Daftar lengkap ada di `ErrorCode` di spec dan dijaga sinkron oleh test backend. Code baru boleh ditambah; arti code lama tidak berubah.
- Error 5xx selalu memakai message generik; detail internal hanya masuk log.
- Kegagalan validasi field memakai `invalid_request` dengan `details.fields = [{field, rule}]` (nama field JSON, dot path untuk field bersarang). Pelanggaran aturan bisnis memakai `validation_failed`.
- Elemen array di body ikut divalidasi per elemen. Contoh: item `POST /v1/payments` yang tidak valid menghasilkan `400 invalid_request` dengan `details.fields` berisi `{field: "items.0.quantity", ...}`, bukan `validation_failed`. Status tetap 400.
- Path id yang bukan integer → `400 invalid_request` (`field: "id"`, `rule: "int"`). Id integer di bawah 1 tidak divalidasi terpisah: tidak cocok dengan baris mana pun, jadi hasilnya `404 not_found` seperti id yang tidak ada. Spec tetap mencantumkan `minimum: 1` sebagai rentang id yang valid. Endpoint dengan ID publik (`pay_…`, dst.) memvalidasi format dan prefix lewat `internal/pkg/id`.
- Query parameter yang tidak valid ditolak (`400 invalid_request`) di list cursor: `limit` di luar 1–200, enum yang tidak dikenal (rule `oneof`), ID publik yang salah format (rule `public_id`), dan waktu yang bukan RFC 3339 (rule `datetime`). List offset lama (`/admin/ledger`, `/admin/webhook-deliveries`) tetap mengabaikan `app_id`/`from`/`to` yang tidak bisa di-parse dan memakai default untuk `limit` di luar rentang, sampai list itu pindah ke cursor. `app_id` yang formatnya benar tapi tidak cocok dengan app mana pun tetap menjadi filter, jadi hasilnya kosong, sama seperti `/admin/payments`.

### Pagination

- List baru memakai cursor: query `limit` (1–200, default 50; di luar rentang → `400 invalid_request`) dan `cursor`; respons `{"data": [...], "next_cursor": "..." | null}`.
- Cursor bersifat opaque: base64url dari JSON berversi yang berisi nilai kolom urutan dan ID publik sebagai tie-breaker, misalnya `(occurred_at, public_id)`. Cursor tidak memuat ID bigint. Cursor rusak → `400 invalid_cursor`.
- Query memakai keyset (`WHERE (sort_col, public_id) < ($1, $2) ORDER BY sort_col DESC, public_id DESC LIMIT n+1`) dengan index yang sesuai, bukan offset.
- Helper ada di `internal/domain/pagination.go` (tipe dan codec cursor) dan transport (parsing query). `/admin/payments` pindah ke cursor di fase 2 dan `/admin/webhook-deliveries` di fase 4; `/admin/ledger` masih offset dan pindah di fase 5.

### Audit log

- Tabel `audit_logs`: `public_id` (`aud_`), `occurred_at`, aktor (`actor_type` = `admin` | `system`, `actor_id`), `action` (`{target}.{verb}`, misalnya `app.update`), target (`target_type`, `target_id` sebagai teks), `before`/`after` JSONB, `ip`, `user_agent`, `request_id`.
- Setiap mutasi admin mencatat satu baris di transaksi yang sama dengan perubahannya, lewat `AuditService`. Aktor dibawa lewat `context` (diisi middleware admin; `system` untuk seed, CLI, dan job). Pencatatan tanpa aktor ditolak.
- Snapshot `before`/`after` dibentuk eksplisit per jenis target dan tidak pernah memuat secret: field rahasia dihilangkan, API key hanya 4 karakter terakhir. Sebagai pengaman kedua, key yang namanya (huruf kecil) mengandung `password`, `secret`, atau `token`, atau berakhiran `_enc`, disamarkan menjadi `"[REDACTED]"` di kedalaman mana pun sebelum disimpan.
- Pengecualian redaksi: key seperti secret yang nilainya boolean atau `null` disimpan apa adanya, karena nilai itu tidak bisa membawa secret. Contohnya `must_change_password` di snapshot `admin_user.change_password`. Nilai string, angka, objek, dan array tetap disamarkan. Dipilih daripada allowlist nama key supaya flag baru tidak perlu didaftarkan satu per satu.
- Sejak fase 1, `actor_type` = `admin` untuk user konsol apa pun role-nya dengan `actor_id` = `usr_…`; `system` untuk seed, CLI, dan setup (`seed`, `cli`, `setup`); `anonymous` (id `anonymous`) untuk login gagal. Entri sebelum fase 1 tetap berisi username admin dan target `admin_user`.
- `GET /admin/audit-logs` (cursor) adalah konsumen pertama cursor pagination dan ditampilkan di Settings.

### Auth, sesi, dan otorisasi

Diputuskan di fase 1.

**User.** Tabel `users`: `public_id` (`usr_`), `email` (disimpan sudah di-trim, Unicode NFC, dan huruf kecil; unik lewat index biasa di kolom itu, jadi pencocokan case-insensitive tidak bergantung pada collation Postgres), `name`, `role` (`owner` | `admin` | `viewer`, CHECK), `password_hash` (bcrypt cost 12), `must_change_password`, `disabled_at`, `last_login_at`, `session_version`, `created_at`, `updated_at`. Tidak ada hard delete; "hapus" = disable, supaya `actor_id` di audit log tetap bisa ditelusuri. Password 12 karakter sampai 72 byte (batas bcrypt), di-hash apa adanya tanpa normalisasi.

**Expand/contract `admin_users` → `users`.** Migrasi `000006` membuat `users` dan menyalin setiap baris `admin_users` menjadi `owner` dengan email `<username>@keipay.invalid` (TLD `.invalid` dicadangkan RFC 2606, jadi tidak pernah menjadi alamat orang lain), password dan `must_change_password` dipertahankan. Kode berhenti membaca `admin_users` di fase ini; tabelnya dibiarkan (read-only) dan di-drop di migrasi contract setelah satu rilis. `down` hanya men-drop `users`.

**Sesi.** Cookie `keipay_session` berisi 32 byte acak (base64url), disimpan di Redis dengan key berupa SHA-256 dari nilai cookie, sehingga dump Redis tidak berisi token yang bisa dipakai. Cookie `HttpOnly`, `SameSite=Lax`, `Path=/`, tanpa `Expires`, `Secure` kecuali `COOKIE_SECURE` (default `auto`) mati di `APP_ENV=development`. Batas idle `SESSION_IDLE_TIMEOUT` (30m) dan absolut `SESSION_ABSOLUTE_TIMEOUT` (12h) dicek dari isi sesi, dan TTL Redis mengikuti yang lebih dulu habis. `last_seen_at` diperbarui paling sering sekali per 60 detik. Setiap request terautentikasi tetap memuat user dari Postgres (satu query PK), sehingga disable dan perubahan role langsung berlaku tanpa daftar sesi per user.

**Pencabutan lewat `session_version`.** Sesi menyimpan `session_version` user saat dibuat. Disable, reset password oleh owner, dan ganti password sendiri menaikkan versi itu, sehingga semua sesi lain langsung ditolak (`401 session_expired`) dan habis sendiri oleh TTL. Ganti password sendiri menerbitkan sesi baru untuk browser yang dipakai. Logout menghapus key sesinya. Kalau Redis atau Postgres gagal saat memuat sesi, request admin mendapat `500 internal_error`, bukan `401`: `401` membuat konsol mengarahkan user yang sebenarnya masih login ke `/login` selama gangguan, sedangkan `500` membiarkannya mencoba lagi. Hanya sesi yang memang tidak valid (tidak ditemukan, kedaluwarsa, versi lama, user disabled) yang berujung `401 session_expired`. Dipilih daripada set sesi per user di Redis karena tetap benar walaupun penghapusan di Redis gagal.

**CSRF.** Double-submit yang diikat ke sesi: cookie `keipay_csrf` (bisa dibaca JS) diterbitkan di respons `/admin/*` mana pun yang request-nya belum membawanya, dan diganti saat login, setup, ganti password, dan logout. Setiap `POST`/`PUT`/`PATCH`/`DELETE` di `/admin/` wajib mengirim header `X-CSRF-Token` yang sama dengan cookie itu, dan kalau ada sesi valid, sama dengan token yang disimpan di sesi (perbandingan constant-time). Ini juga berlaku untuk login dan setup, supaya tidak ada login CSRF. Gagal → `403 invalid_csrf_token`. Konsol memanggil `GET /admin/setup` dulu kalau cookie belum ada.

**Rate limit login.** Counter fixed window di Redis untuk login yang gagal: per IP (default 20) dan per email kanonik (default 5) dalam `LOGIN_FAILURE_WINDOW` (15m). Dicek sebelum password diperiksa, jadi respons `429 rate_limited` + `Retry-After` tidak membocorkan apakah password benar. Login sukses menghapus counter email, tidak counter IP. Hanya kegagalan yang dihitung, supaya banyak admin di balik satu NAT tidak saling mengunci; burst paralel bisa sedikit melewati batas, dan itu diterima. Redis tidak tersedia → login gagal 500 (sesi juga butuh Redis). Setup memakai counter IP yang sama: token setup yang salah dihitung sebagai kegagalan, dan setup yang ditolak batas IP dicatat sebagai security event `auth.setup_rate_limited` (login: `auth.login_rate_limited`). IP klien = peer TCP kecuali proxy terdaftar di `TRUSTED_PROXIES`. Email yang tidak dikenal tetap menjalankan bcrypt terhadap hash dummy supaya waktu respons tidak membocorkan keberadaan akun. User disabled mendapat `401 invalid_credentials` yang sama.

**First-run setup.** `POST /admin/setup` membuat owner pertama hanya kalau tabel `users` kosong, di transaksi dengan `pg_advisory_xact_lock` supaya dua request bersamaan hanya menghasilkan satu owner; berikutnya `409 setup_completed`. `SETUP_TOKEN` opsional: kalau di-set, setup wajib menyertakannya. `make seed` tidak lagi membuat admin. `-reset-admin -email <email> [-password <pw>]` adalah jalur pemulihan dari shell server: set password user itu (acak kalau kosong, dan saat itu `must_change_password` = true), enable kembali, naikkan `session_version`; kalau belum ada user sama sekali, membuat owner.

**Otorisasi.** Permission per role adalah data di `internal/domain` (`RolePermissions`), bukan `if role == ...` yang tersebar. Setiap route admin di `router.go` mendeklarasikan satu permission atau "cukup login"; `GET /admin/me` mengembalikan daftar permission sehingga UI tidak menghardcode matriks. Spec mencatat permission per operasi di `x-keipay-permission`, dan test backend membandingkan route, permission, dan spec supaya tidak drift.

| Permission | owner | admin | viewer |
|---|---|---|---|
| `apps:read`, `connections:read`, `payments:read`, `deliveries:read` | ya | ya | ya |
| `apps:write`, `connections:write`, `deliveries:replay`, `audit:read` | ya | ya | tidak |
| `users:manage` | ya | tidak | tidak |

Viewer tidak membaca audit log karena isinya (email, IP, perubahan user) setara dengan manajemen user. Fase 2 mengganti `tenants:read`/`tenants:write` dengan `apps:read`/`apps:write` (route tenant dihapus, jadi permission lamanya ikut hilang; konsol adalah satu-satunya klien `/admin`). Fase berikutnya menambah permission per resource (`connections:*`, dst.) dengan pola yang sama.

Aturan user: selalu minimal satu owner aktif (`409 last_owner` untuk demote atau disable owner aktif terakhir); owner tidak bisa men-disable dirinya sendiri atau me-reset password sendiri lewat API users (`409 cannot_modify_self`). Validasi `role` di `POST`/`PATCH /admin/users`: kosong → rule `required`, nilai di luar `owner`/`admin`/`viewer` → rule `oneof` (keduanya `400 invalid_request`). Semua mutasi user dijalankan di transaksi dengan advisory lock yang sama dengan setup, supaya cek owner terakhir tidak bisa di-race. User dengan `must_change_password` hanya boleh memanggil `GET /admin/me`, `POST /admin/me/password`, dan `POST /admin/logout` (`403 password_change_required`), dicek di server.

Audit: `user.setup`, `user.create`, `user.update`, `user.disable`, `user.enable`, `user.reset_password`, `user.change_password`, `user.login`, `user.login_failed` (actor `anonymous`, `after` berisi email kanonik dan alasan: `invalid_credentials` atau `user_disabled`), `user.logout`. Login yang ditolak rate limit tidak diaudit (hanya security log), supaya audit log tidak bisa dibanjiri.

Dependensi: `golang-jwt` dihapus (JWT tidak dipakai lagi), `golang.org/x/text` dipakai langsung untuk normalisasi NFC email. Frontend tidak menambah library.

### Apps, API key, dan identifier

Diputuskan di fase 2. Tujuannya: KeiPay selalu tahu sebuah request, payment, atau webhook milik app mana lewat lookup tabel, tidak lewat parsing string.

**Expand/contract `tenants` → `apps`.** Migrasi `000007` membuat tabel baru `apps` dan menyalin setiap baris `tenants` dengan `id` yang sama (sequence `apps` dimajukan sesudahnya), `public_id` `app_<ULID>` dari `created_at`, `environment = live`, `status` dari `active`, dan `webhook_secret_enc` = `api_secret_enc` lama. `payments`, `webhook_deliveries`, dan `ledger_entries` mendapat kolom `app_id` (FK ke `apps`, di-backfill dari `tenant_id`, lalu `NOT NULL`), sedangkan `tenant_id` menjadi nullable dan tidak ditulis lagi. Tabel `tenants` dan kolom `tenant_id` dibiarkan read-only, lalu di-drop di migrasi contract setelah satu rilis, sama seperti `admin_users`. Dipilih daripada `RENAME TABLE` supaya rollback (`down`) dan binary lama tetap punya data aslinya. Deploy tetap urutan biasa: hentikan, migrate, jalankan. `down` mengembalikan data app ke `tenants` sebisanya; key yang dicabut setelah migrasi hidup lagi dan app baru mendapat `api_key` placeholder yang tidak bisa dipakai, karena `down` hanya alat development.

Legacy tenant menjadi `live` karena integrasi yang sudah ada memakai akun Xendit sungguhan. Fase 3 boleh menyesuaikan environment app hasil migrasi kalau connection Xendit dari env ternyata kunci development (`xnd_development_…`), sebelum aturan "environment app = environment connection" ditegakkan.

**App.** `public_id` (`app_`), `slug`, `name`, `environment` (`test` | `live`), `status` (`active` | `disabled`), `metadata` JSONB, `webhook_url`, `redirect_success_url`, `redirect_failure_url`, `webhook_secret_enc`. `slug` unik per environment (`UNIQUE (environment, slug)`), jadi `acme` boleh ada di test dan live. `slug` dan `environment` tidak pernah berubah: `PATCH` yang mengirim salah satunya ditolak dengan rule `immutable`, bukan diabaikan, supaya pemanggil tahu perubahannya tidak terjadi. Disable/enable punya endpoint sendiri (aksi audit sendiri, konfirmasi di UI). Kosakata environment `test`/`live` juga dipakai connection di fase 3 (bukan `sandbox`/`production` seperti sketsa data model di atas), supaya aturan "environment app = environment connection" cukup membandingkan string.

**API key.** Tabel `app_api_keys`: `public_id` (`key_`), `app_id`, `environment`, `label`, `key_prefix` (`kp_test_`, `kp_live_`, atau `kp_` untuk key hasil migrasi), `key_last4`, `key_hash` (hex SHA-256 dari key utuh, `UNIQUE`), `secret_enc` (AES-256-GCM), `last_used_at`, `expires_at`, `revoked_at`. Format key `kp_<env>_` + 32 hex, secret `kps_<env>_` + 48 hex. Key dicari lewat hash-nya, jadi dump database tidak berisi key yang bisa dipakai; key dan secret hanya ditampilkan sekali. Prefix `kps_`/`kpwh_` sengaja tidak memakai `sk_`/`whsec_` supaya secret scanner tidak salah mengira secret KeiPay sebagai secret Stripe.

- **Environment dijaga tiga lapis.** FK komposit `(app_id, environment) → apps (id, environment)` membuat key tidak mungkin tersimpan dengan environment berbeda dari app-nya, `CHECK` mengikat `key_prefix` ke `environment`, dan middleware `/v1` menolak (sebagai `unknown_api_key`, dengan security event) kalau prefix di header tidak cocok dengan environment baris key atau app.
- **Status turunan.** `revoked` kalau `revoked_at` terisi, `expired` kalau `expires_at` sudah lewat, selain itu `active`. Paling banyak 10 key usable per app (dicek di transaksi yang mengunci baris app), supaya rotasi yang lupa mencabut key lama tetap terbatas.
- **Rotasi tumpang tindih.** Rotate membuat key baru dengan label yang sama dan tidak menyentuh key lama; key lama berlaku sampai dicabut atau kedaluwarsa. Alurnya: rotate, pasang key baru di app, cabut key lama. Tidak ada expiry otomatis untuk key lama, karena app yang belum sempat deploy akan putus tanpa peringatan.
- **Urutan cek `/v1`.** Header, timestamp, lookup key (`unknown_api_key`), signature, lalu status key (`api_key_revoked`, `api_key_expired`) dan app (`tenant_inactive`), lalu nonce. Status dicek sesudah signature supaya hanya pemegang secret yang tahu sebuah key dicabut. Code `tenant_inactive` dipertahankan untuk app yang disabled karena code tidak boleh berubah arti atau hilang.
- **Tanpa cache key.** Cache tenant di Redis dihapus: satu query `JOIN` key + app lewat index unik `key_hash` per request sudah murah, dan pencabutan berlaku seketika tanpa invalidasi. (Cache lama juga menyimpan tenant tanpa `api_secret_enc`, sehingga request kedua dalam 5 menit gagal; masalah itu hilang bersama cache-nya.) `last_used_at` diperbarui dengan `UPDATE … WHERE last_used_at IS NULL OR last_used_at < now() - interval '1 minute'`, best effort: kegagalannya hanya dicatat di log.

**Webhook signing secret per app.** Karena app sekarang punya banyak key dengan secret berbeda, webhook keluar tidak lagi ditandatangani dengan secret key. Setiap app punya `webhook_secret_enc` sendiri (`kpwh_` + 48 hex, ditampilkan sekali saat app dibuat); app hasil migrasi memakai `api_secret` lamanya, jadi verifikasi `X-Kei-Signature` di app yang sudah ada tidak berubah. Rotasi API key tidak pernah memutus verifikasi webhook. Rotasi secret webhook belum ada di fase 2; fase 4 memindahkan secret ini ke `webhook_endpoints` hasil migrasi dan menambah rotasinya.

**Payment.** `payments` mendapat `public_id` (`pay_`, backfill dari `created_at`), `app_id`, `metadata` JSONB (default `{}`), `idempotency_key`, dan `idempotency_fingerprint`. Index unik baru `(app_id, order_id)` dan `(app_id, idempotency_key) WHERE idempotency_key IS NOT NULL`, jadi dua app boleh memakai `order_id` atau key yang sama.

- **Reference ke gateway** untuk payment baru adalah `kp_<public_id>` (misalnya `kp_pay_01JB…`), disimpan di `payments.external_id` dan dikirim sebagai `external_id` Xendit. Callback dicari dengan `WHERE external_id = $1` lewat index unik yang sudah ada. Payment lama yang memakai `{slug}:{order_id}` ditemukan dengan query yang sama karena nilainya tersimpan di kolom itu; tidak ada kode yang mem-parse slug. Payment lama yang tidak ditemukan tetap `500` supaya Xendit mengulang, seperti sebelumnya. Fase 6 memindahkan reference ke `payment_attempts` dan lookup ini ikut pindah.
- **Idempotency.** Kalau `Idempotency-Key` dikirim, key dicek lebih dulu: key sama dan fingerprint sama → payment lama (`201` + `Idempotent-Replayed: true`); key sama dengan fingerprint berbeda → `409 idempotency_key_reused`. Fingerprint = hex SHA-256 dari JSON kanonik input yang sudah divalidasi (`order_id`, `amount`, `currency`, `description`, `payer_email`, `invoice_duration`, `items`, `customer`, `expected_settlement_at`, `metadata`), bukan raw body, supaya urutan key JSON atau spasi tidak membuat retry yang sah ditolak. Kalau tidak ada key yang cocok, aturan `order_id` lama berlaku apa adanya (amount dan currency sama → payment lama, beda → `409 conflict`), sehingga integrasi lama tidak berubah perilakunya. Request ber-key yang jatuh ke payment lama lewat `order_id` tidak menempelkan key-nya ke payment itu. Key tidak kedaluwarsa. Pembuatan invoice tetap seperti sebelumnya (buat di Xendit, lalu insert; kalah race → expire invoice yatim dan kembalikan pemenangnya kalau cocok).
- **Metadata** (payment dan app) adalah objek datar: maksimal 50 key, key 1–40 code point tanpa karakter kontrol, nilai string (maksimal 500 code point, tanpa U+0000 karena Postgres JSONB menolaknya), angka, boolean, atau `null`, total maksimal 8192 byte JSON kompak. Angka di-decode dengan `json.Number` supaya tidak dibulatkan lewat float; urutan key tidak dijaga (JSONB mengurutkan ulang). Objek datar dipilih karena cukup untuk tag dan ID eksternal, mudah ditampilkan di konsol, dan bisa dilonggarkan nanti tanpa breaking change. Metadata payment ikut dikirim di setiap webhook payment itu.
- **Kontrak `/v1` hanya bertambah.** Respons payment menambah `id` (`pay_…`), `reference`, dan `metadata`; `payment_id` integer tetap ada. Payload webhook v1 menambah `payment_public_id` dan `metadata`. Envelope berversi datang di fase 4.

**Admin.** `/admin/tenants` dihapus dan diganti `/admin/apps`, `/admin/apps/{id}`, disable/enable, dan `/admin/apps/{id}/keys` (list, create, rotate, revoke). `/admin/payments` pindah ke cursor dengan filter `app_id`, `status`, `from`, `to` yang divalidasi ketat dan tanpa `total` (count penuh mahal dan tidak dibutuhkan UI). Baris payment, ledger, dan delivery membawa ringkasan app (`{id, slug, name, environment}`) hasil `JOIN`, jadi konsol tidak perlu memuat semua app untuk menampilkan nama.

Audit: `app.create` dan `api_key.create` (dua baris di satu transaksi saat app dibuat), `app.update` (hanya field yang berubah), `app.disable`, `app.enable`, `api_key.create`, `api_key.rotate` (target key baru, `before` key lama, `after` key baru), `api_key.revoke`. Snapshot key berisi `id`, `app_id`, `label`, `environment`, `prefix`, `last4`, `expires_at`, `revoked_at`, tanpa key utuh, hash, maupun secret. Entri lama `tenant.*` tetap seperti adanya.

**Konsol.** Switch Test/Live di top bar sekarang punya arti: list Apps memakai mode itu sebagai filter environment awal (chip environment tetap bisa diganti ke All), dan dialog buat app memilih environment sesuai mode. Fase 3 menerapkan hal yang sama ke list Connections dan wizard tambah connection. Payments, Webhooks, dan Reconciliation belum difilter per mode; filternya ditambahkan saat halaman itu dikerjakan ulang (fase 4 dan 5), lewat join ke `apps.environment`, bukan kolom environment baru di `payments`.

### Connections dan provider

Diputuskan di fase 3. Tujuannya: menambah gateway cukup dengan satu paket adapter dan satu baris registrasi, tanpa mengubah frontend.

**Port.** `internal/provider` berisi port `Provider`, `Registry`, `Capabilities`, schema field, dan kelas error (`ErrRejected`, `ErrRetryable`, `ErrUnknownOutcome`, `ErrNotFound`, dibungkus dengan `%w` dan dicek dengan `errors.Is`). Perbedaan dari sketsa di [Port `provider.Provider`](#port-providerprovider):

- Setiap call ke PG menerima `provider.Account{Environment, Credentials, Settings}`, bukan `Credentials` saja, karena setting connection (misalnya durasi invoice default) ikut menentukan request.
- Adapter mendeklarasikan `CredentialSchema()` dan `SettingsSchema()` dalam satu format (`SchemaField`: `key`, `type` `string|integer|boolean|enum`, `label`, `description`, `placeholder`, `secret`, `required`, `default`, `options`, `min_length`, `max_length`, `pattern` + `pattern_hint`, `minimum`, `maximum`). Validator generik di `internal/provider` dipakai service untuk create dan update; `ValidateAccount(env, creds)` di adapter menambah aturan lintas field (Xendit: prefix `xnd_development_` hanya untuk `test`, `xnd_production_` hanya untuk `live`). Key yang tidak ada di schema ditolak (`unknown`), supaya salah ketik tidak tersimpan diam-diam. Nilai kredensial selalu string dan di-trim.
- `Descriptor()` mengembalikan nama tampilan, deskripsi, URL dokumentasi, dan langkah memasang URL webhook di dashboard PG. `GET /admin/providers` menyajikan semuanya; konsol tidak punya kode per provider.
- `FindByReference` dan `GetStatus` belum ada di port. Keduanya ditambahkan saat dipakai (rekonsiliasi di fase 5, attempt loop di fase 6); adapter ada di repo yang sama, jadi menambah method ke interface tidak memutus siapa pun.
- `MapStatus` Xendit tetap memetakan status yang tidak dikenal ke `PENDING` (perilaku lama, efektifnya diabaikan sebagai stale). Status `unmapped` yang tercatat datang bersama `inbound_webhooks` di fase 4.

**Data.** Migrasi `000008`:

- `merchants`: satu baris default (`is_default`, unik parsial), tanpa ID publik dan tidak tampil di UI. `legacy_connection_id` menunjuk connection hasil impor env, target alias `/webhooks/xendit`.
- `connections`: `public_id` (`con_`), `merchant_id`, `provider`, `name` (format seperti slug, 3–64 karakter, unik per merchant; boleh diganti di fase 3, dan fase 6 memutuskan ulang kalau app mulai merujuk connection lewat nama), `environment` (`test` | `live`, kosakata yang sama dengan app), `status` (`active` | `disabled`), `credentials_enc`, `credentials_dek_enc`, `key_version`, `credentials_last4` JSONB, `settings` JSONB, `last_tested_at`, `last_test_status` (`ok` | `failed`), `last_test_error`. `UNIQUE (id, merchant_id, environment)` menjadi target FK komposit dari `apps`.
- `connection_channels`: `(connection_id, channel)`, `enabled`. Fee, batas amount, dan jadwal maintenance dari sketsa data model belum dibuat; kolomnya ditambahkan secara additive saat fiturnya ada (fase 5, 6, 9), supaya tidak ada konfigurasi yang tersimpan tanpa efek.
- `apps.merchant_id` (backfill ke merchant default, lalu `NOT NULL`) dan `apps.default_connection_id`, dengan FK komposit `(default_connection_id, merchant_id, environment) → connections (id, merchant_id, environment)`. Jadi app tidak mungkin menunjuk connection milik merchant lain atau beda environment, bahkan lewat SQL langsung. Service tetap memeriksa lebih dulu supaya error-nya berupa field error yang jelas.
- `payments.connection_id` dan `payments.provider` (nullable). Payment baru selalu mengisinya; payment lama diisi saat impor env.

**Envelope encryption.** Setiap connection punya data key acak 32 byte. Kredensial (JSON) dienkripsi AES-256-GCM dengan data key itu dan `public_id` connection sebagai additional data, sehingga ciphertext yang dipindah ke baris lain gagal didekripsi. Data key dienkripsi dengan master key `ENCRYPTION_KEY` lewat `crypto.Cipher` yang sudah ada. `key_version = 1`; rotasi master key cukup mengenkripsi ulang data key. Respons admin hanya membawa 4 karakter terakhir per field (`null` kalau nilainya kurang dari 12 karakter, karena 4 karakter dari nilai sependek itu terlalu banyak membuka), disimpan di `credentials_last4` saat ditulis sehingga list dan detail tidak perlu dekripsi. Snapshot audit tidak pernah memuat kredensial maupun last4: `connection.update_credentials` hanya mencatat nama field yang diganti.

**Impor env (sekali).** Kalau `XENDIT_SECRET_KEY` dan `XENDIT_CALLBACK_TOKEN` di-set dan merchant default belum punya `legacy_connection_id`, server saat start membuat satu connection Xendit (nama `xendit-imported`, atau `xendit-imported-<6 karakter terakhir ID>` kalau nama itu sudah dipakai), dengan environment dari prefix key, lalu dalam transaksi yang sama: mengisi `legacy_connection_id`, mengisi `payments.connection_id`/`provider` yang masih kosong, dan menjadikannya default connection untuk setiap app yang belum punya default dan environment-nya sama. Transaksi mengunci baris merchant, jadi dua instance yang start bersamaan hanya mengimpor sekali. Impor dicatat di audit sebagai `system:import` (`connection.create`, dan `app.update` per app). Setelah itu env diabaikan, dan server memberi peringatan setiap start supaya env dihapus. Kalau hanya satu dari dua env yang di-set, config gagal dimuat. Key dengan prefix yang tidak dikenal tidak diimpor (error di log, server tetap jalan). App hasil migrasi fase 2 (`live`) tidak dipindah environment-nya walaupun key-nya `xnd_development_`: memindah environment app juga harus memindah semua API key-nya (FK komposit) dan mengubah arti key yang sudah dipakai integrasi. App seperti itu tidak mendapat default connection, dan server mencatat peringatan berisi daftar app tersebut. Konsekuensinya: deploy yang sudah ada harus tetap menyetel kedua env itu saat start pertama setelah upgrade.

**Webhook masuk per connection.** `POST /webhooks/{provider}/{connection_id}` me-resolve connection lewat ID publik, mewajibkan provider di path sama dengan provider connection (selain itu `404`), lalu `VerifyWebhook` dengan kredensial connection itu sebelum body dibaca. Token yang valid untuk connection lain ditolak `401 invalid_callback_token` dan dicatat sebagai security event. Payment hasil lookup reference harus milik connection yang sama; kalau tidak, callback dicatat tetapi tidak di-apply (security event, `200` ke PG), sama seperti mismatch amount. Connection yang disabled tetap menerima callback supaya payment lama tetap ter-update. `/webhooks/xendit` adalah alias ke `legacy_connection_id` (`404` kalau tidak ada), sehingga dashboard Xendit yang belum diubah tetap jalan. Dedupe event tetap `(xendit_id, status)` global sampai `inbound_webhooks` di fase 4 (sejak itu per connection, lihat [Webhook router](#webhook-router)). Satu akun PG sebaiknya hanya punya satu connection: ganti kredensial di connection yang sama, jangan membuat connection kedua untuk akun yang sama, karena PG hanya mengirim callback ke satu URL dan payment milik connection lama akan dianggap mismatch.

**Create payment.** Replay idempotent dijawab sebelum connection dilihat. Sesudah itu payment memakai `default_connection_id` app: tidak ada → `422 no_eligible_connection` (`reasons: ["no_default_connection"]`), disabled → `["connection_disabled"]`. Kalau channel yang aktif adalah subset ketat dari channel provider, checkout dibatasi ke channel itu; kalau semua aktif atau belum diatur, checkout tidak dibatasi, karena mengirim daftar lengkap bisa membuat Xendit menolak metode yang belum diaktifkan di akun. Error adapter dipetakan: `ErrRejected` → `422 provider_rejected`, `ErrRetryable` dan `ErrUnknownOutcome` → `503 provider_unavailable` (sebelumnya semua `500`). Pesan PG hanya masuk log. `ErrUnknownOutcome` saat create masih bisa meninggalkan invoice di Xendit tanpa baris payment, sama seperti sebelumnya; attempt dan adopsi di fase 6 yang menyelesaikannya.

**Test connection.** `POST /admin/connections/{id}/test` memanggil `TestCredentials` (Xendit: list satu invoice, permission yang memang dibutuhkan) dengan timeout 10 detik dan selalu menjawab `200` dengan `result` `ok`/`failed` plus pesan yang sudah disanitasi. Hasilnya disimpan di connection dan dicatat `connection.test`. Mengganti kredensial mengosongkan hasil test.

**Otorisasi.** `connections:read` (owner, admin, viewer) dan `connections:write` (owner, admin). Memilih default connection app memakai `apps:write`. Disable connection ditolak `409 connection_in_use` selama ada app (aktif maupun disabled) yang memakainya sebagai default, supaya default connection app selalu aktif.

Audit: `connection.create`, `connection.update` (hanya field yang berubah), `connection.update_credentials`, `connection.update_channels`, `connection.disable`, `connection.enable`, `connection.test`, dan `app.update` untuk `default_connection_id`. Snapshot connection berisi `id`, `provider`, `name`, `environment`, `status`, `settings`, tanpa kredensial.

**Konsol.** Wizard tambah connection adalah halaman (`/connections/new`), bukan dialog, supaya nyaman di mobile: provider dan environment → form kredensial dan setting dari schema → connection dibuat lalu langsung dites → URL webhook + petunjuk provider → channel. Langkah setelah connection dibuat boleh dilewati karena semuanya bisa diatur di halaman detail. Form dirender generik dari `ConfigSchema`; field secret memakai input password dengan tombol tampilkan dan tidak pernah diisi ulang dari server (saat ganti kredensial, field kosong berarti "tetap").

### Webhook router

Diputuskan di fase 4. Tujuannya: setiap app menentukan sendiri ke mana dan event apa saja yang dikirim KeiPay, dan setiap webhook masuk dari PG bisa ditelusuri.

**Katalog event.** Daftar event adalah data di `internal/domain` (`EventCatalog`: tipe, deskripsi, contoh `data`), disajikan lewat `GET /admin/event-types` beserta wildcard yang diterima. Konsol membangun pemilih event dari respons itu, tidak menghardcode daftar. Tipe: `payment.paid`, `payment.settled`, `payment.expired`, `payment.failed` (dari `"payment." + lower(status)`), dan `payment.duplicate`. `payment.duplicate` sudah ada di katalog supaya app bisa subscribe sejak sekarang, tetapi baru dikirim mulai fase 6 (attempt kedua yang dibayar); sebelum itu tidak ada alur yang tahu sebuah payment dibayar dua kali.

**Subscription.** Setiap endpoint menyimpan daftar (`TEXT[]`, 1–20 entri) berisi tipe persis, wildcard grup (`payment.*`, hanya untuk grup yang ada di katalog), atau `*` (semua tipe, termasuk tipe baru di masa depan). Pencocokan dilakukan di Go (`domain.MatchSubscription`), bukan SQL, karena jumlah endpoint per app kecil (maksimal 10).

**Endpoint (`webhook_endpoints`, `whe_`).** Kolom: `app_id`, `url`, `description`, `events`, `api_version` (`v1` | `v2`), `legacy_signature`, `secret_enc` + `secret_last4`, `previous_secret_enc` + `previous_secret_expires_at`, `headers_enc` (JSON `[{name, value}]` terenkripsi) + `headers_hint` (JSONB `[{name, value_last4}]` untuk tampilan tanpa dekripsi), `status` (`active` | `disabled`), `disabled_reason` (`manual` | `consecutive_failures`), `disabled_at`, `consecutive_failures`, `retry_max_attempts`, `retry_backoff`, `retry_interval_seconds`, `migrated`, `deleted_at`. Maksimal 10 endpoint yang belum dihapus per app (`400 validation_failed`, sama seperti batas API key). Hapus = soft delete (`deleted_at`), supaya delivery lama tetap bisa dibaca dan audit tetap bisa ditelusuri. URL mengikuti aturan `HttpUrl`; app `live` wajib `https` (rule `https`). Alamat privat tidak diblokir: KeiPay self-hosted dan penerimanya sering ada di jaringan internal, dan URL hanya bisa diisi admin.

- **Header kustom** (maksimal 10) disimpan terenkripsi karena nilainya sering berupa token (`Authorization`). Respons hanya membawa nama dan 4 karakter terakhir (`null` kalau nilainya kurang dari 12 karakter), dan PATCH mengganti seluruh daftar. Nama yang dipakai KeiPay sendiri (`Content-Type`, `Content-Length`, `Host`, `User-Agent`, `Connection`, `Transfer-Encoding`, `X-KeiPay-*`, `X-Kei-*`) ditolak (rule `reserved`). Log attempt menampilkan nilainya sebagai `[REDACTED]`.
- **Secret** `kpwh_` + 48 hex, dienkripsi dengan `crypto.Cipher` yang sama dengan secret app lama (tanpa additional data), sehingga migrasi bisa menyalin ciphertext `apps.webhook_secret_enc` apa adanya. Endpoint hasil migrasi tidak punya `secret_last4` (plaintext tidak tersedia di SQL) sampai secret-nya dirotasi.
- **Rotasi tumpang tindih.** Rotate membuat secret baru (ditampilkan sekali), memindahkan secret lama ke `previous_secret_*` dengan masa berlaku `overlap_hours` (0–168, default 24), dan membuang previous yang lebih lama. Selama masa itu `X-KeiPay-Signature` membawa dua `v1=` (baru dulu), jadi penerima yang masih memakai secret lama maupun yang sudah memakai secret baru sama-sama lolos. `X-Kei-Signature` hanya satu nilai, jadi selalu memakai secret terbaru; README meminta pindah ke `X-KeiPay-Signature` sebelum rotasi.
- **Kebijakan retry** per endpoint: `max_attempts` 1–20 (default 8), `backoff` `exponential` (`interval × 2^(n-1)`, maksimal 6 jam) atau `fixed`, `interval_seconds` 10–3600 (default 30). Default ini sekitar satu jam total. Tanpa jitter: jumlah delivery per endpoint kecil dan perilaku yang deterministik lebih mudah dites dan dijelaskan. `WEBHOOK_MAX_RETRY` tidak dibaca lagi.

**Event dan envelope.** Setiap perubahan status yang di-apply membuat satu baris `events` (`evt_`, `type`, `api_version` = versi `data` = `v2`, `app_id`, `payment_id`, `data` JSONB, `is_test`) di transaksi yang sama dengan update payment. Envelope `v2` adalah `{id, type, api_version, created_at, data}` dengan `data` = snapshot payment (`id`, `order_id`, `reference`, `status`, `amount`, `currency`, `metadata`, `provider`, `connection_id`, `paid_at`, `settled_at`). Ini menggantikan sketsa payload datar di [Webhook keluar (v2)](#webhook-keluar-v2): field datar lama (`event`, `event_id`, `payment_id`, `occurred_at`) pindah ke envelope atau ke `data`. Field `channel` dan `provider_fee` dari sketsa belum ada karena datanya belum ada; ditambahkan secara additive saat tersedia (fase 5 dan 6).

- **Body per versi.** Endpoint `v2` menerima envelope; endpoint `v1` menerima body datar lama (`PaymentWebhookEvent`) ditambah `event_id`. Body dirender sekali saat fan-out sesuai `api_version` endpoint saat itu dan disimpan di `webhook_deliveries.payload`, sehingga setiap attempt dan replay mengirim byte yang sama, dan detail delivery menunjukkan persis apa yang dikirim. Mengganti `api_version` hanya berlaku untuk delivery berikutnya.
- **Header.** `X-KeiPay-Event-Id`, `X-KeiPay-Event-Type`, `X-KeiPay-Delivery-Id`, `X-KeiPay-Signature: t=<ts>,v1=<hex HMAC-SHA256("{ts}.{body}")>`, `User-Agent: KeiPay-Webhooks/1`, dan `X-KeiPay-Test: true` untuk kirim uji. Endpoint dengan `legacy_signature` juga mendapat `X-Kei-Signature` (HMAC body saja) dan `X-Kei-Event`. Nama header memakai `KeiPay`, bukan sketsa `X-KeiPay-Timestamp` terpisah: timestamp ada di dalam signature header supaya tidak bisa dipisah dari tanda tangannya.

**Fan-out.** `WebhookService.Process` memuat endpoint aktif (belum dihapus) milik app dalam satu query, mencocokkan subscription, lalu membuat satu `webhook_deliveries` per endpoint yang cocok, di transaksi yang sama. Setelah commit semua delivery di-enqueue. App tanpa endpoint yang cocok tetap mendapat baris `events` (berguna untuk timeline fase 6), tanpa delivery.

**Delivery (`webhook_deliveries`, expand).** Tabel lama diperluas, tidak dibuat ulang: `public_id` (`dlv_`, backfill dari `created_at`), `event_id`, `endpoint_id`, `max_attempts`, `next_attempt_at`, `last_attempt_at`, `last_response_status`; `payment_id` menjadi nullable karena kirim uji tidak punya payment. Status tetap `PENDING` | `DELIVERED` | `FAILED`, tetapi `PENDING` sekarang juga berarti "menunggu retry" (`next_attempt_at`), dan `FAILED` hanya berarti selesai gagal (sebelumnya `FAILED` ditulis setiap kali attempt gagal walaupun asynq masih akan mengulang). Setiap attempt dicatat di `webhook_delivery_attempts` (nomor, manual atau tidak, URL, header request dengan nilai kustom disamarkan, status dan 4096 byte pertama body respons, error, durasi). Retensi log belum ada; dibahas bersama dashboard di fase 9.

- **Retry dikendalikan KeiPay, bukan asynq.** Worker mengirim, mencatat attempt, lalu sendiri menjadwalkan attempt berikutnya (`next_attempt_at`, task asynq dengan `ProcessAt`) atau menandai `FAILED`. Task ID `webhook:forward:{delivery_id}:{attempt}` membuat setiap attempt unik, dan worker mengabaikan task yang nomor attempt-nya tidak sama dengan `attempts + 1` (task basi). Update hasil attempt memakai guard `WHERE attempts = $n AND status = 'PENDING'`, jadi dua worker tidak bisa mencatat attempt yang sama. Error yang dikembalikan ke asynq hanya untuk gangguan infrastruktur (database), dengan `MaxRetry` 10. Alasannya: kebijakan retry per endpoint, jadwal yang terlihat di UI, dan replay yang tidak tergantung pada task yang sudah diarsipkan asynq.
- **Respons yang dihitung sukses** hanya `2xx` dalam 15 detik. Redirect tidak diikuti (dihitung gagal), supaya signature dan header kustom tidak terkirim ke host lain.
- **Sweeper** sekarang mengambil delivery `PENDING` yang `next_attempt_at`-nya sudah lewat lebih dari 2 menit (enqueue setelah commit gagal, Redis di-flush, atau task diarsipkan), lalu enqueue ulang dengan task ID yang sama. Kalau ID itu masih dipegang task yang diarsipkan, task itu dihapus lewat `asynq.Inspector` lalu di-enqueue lagi. Index lama `idx_deliveries_unsent` diganti `idx_deliveries_due (next_attempt_at) WHERE status = 'PENDING'`.
- **Nonaktif otomatis.** `consecutive_failures` di endpoint naik setiap kali sebuah delivery habis attempt-nya, dan kembali 0 pada attempt mana pun yang sukses. Saat mencapai `WEBHOOK_DISABLE_AFTER_FAILURES` (default 5; 0 = tidak pernah), endpoint dinonaktifkan dengan `disabled_reason = consecutive_failures` di transaksi yang sama dan dicatat di audit sebagai `system:worker` (`webhook_endpoint.auto_disable`). Dihitung per delivery, bukan per attempt, supaya satu event yang gagal 8 kali tidak langsung mematikan endpoint. Delivery `PENDING` milik endpoint yang disabled atau dihapus menjadi `FAILED` saat gilirannya tiba, tanpa attempt. Enable mengembalikan `consecutive_failures` ke 0; delivery yang gagal tidak dikirim ulang otomatis.
- **Replay** hanya untuk delivery `DELIVERED` atau `FAILED`: `max_attempts = attempts + 1`, status `PENDING`, satu attempt manual tanpa retry, dan tidak memengaruhi `consecutive_failures`. Event id, delivery id, dan body tetap sama, jadi replay idempotent dari sisi penerima yang dedupe per `event_id`. Replay tidak membuat event atau delivery baru. Selain itu ditolak `409 delivery_not_replayable` dengan `details.reason` (`in_progress`, `no_endpoint`, `endpoint_deleted`, `endpoint_disabled`).
- **Kirim uji** membuat event `is_test` dengan `data` dari contoh katalog dan satu delivery ke endpoint itu saja (tanpa melihat subscription maupun status), `max_attempts = 1`, tidak memengaruhi `consecutive_failures`, dan membawa `X-KeiPay-Test: true`.

**Migrasi `apps.webhook_url` (expand).** Migrasi `000009` membuat satu endpoint per app yang punya `webhook_url`: subscribe `*`, `api_version = v1`, `legacy_signature = true`, `migrated = true`, `secret_enc` = `apps.webhook_secret_enc`. Integrasi lama menerima body dan `X-Kei-Signature` yang sama seperti sebelumnya, ditambah header baru. Delivery lama mendapat `public_id`, `endpoint_id` = endpoint hasil migrasi app-nya (kalau ada), dan `max_attempts`; `event_id` tetap `NULL` karena dibuat sebelum ada events. Delivery lama yang masih `PENDING` mendapat `next_attempt_at = now()` dan sisa enam attempt, sehingga sweeper mengirimnya dengan mekanisme baru; task asynq lama (`{"delivery_id": n}`, tanpa nomor attempt) tetap diterima worker. `apps.webhook_url` dan `apps.webhook_secret_enc` tidak dibaca maupun ditulis lagi (kolom secret mendapat default `''`) dan di-drop di migrasi contract setelah satu rilis. API admin app tidak lagi menerima `webhook_url` (rule `moved`) dan create app tidak lagi mengembalikan `webhook_secret`; konsol adalah satu-satunya klien `/admin`, jadi perubahan ini tidak memutus integrasi.

**Webhook masuk (`inbound_webhooks`, `inb_`).** Setiap callback ke connection yang dikenal dicatat: `connection_id`, `provider`, `source` (`webhook`; `poll` di fase 6), `signature_valid`, `result`, `result_detail`, `event_key`, `provider_status`, `reference`, `payment_id`, `event_id`, `raw_body` (`BYTEA`, byte persis seperti diterima), `body_truncated`, `received_at`. Callback ke connection yang tidak dikenal (`404`) tidak dicatat supaya tabel tidak bisa dibanjiri lewat URL tebakan, dan body yang gagal verifikasi disimpan maksimal 4096 byte untuk alasan yang sama. `result`: `applied`, `stale`, `duplicate`, `mismatch`, `unmapped`, `invalid_signature`, `invalid_payload`, `error`.

- **Dedupe pindah ke tabel ini dan dibatasi per connection.** Index unik parsial `(connection_id, event_key, provider_status) WHERE result IN ('applied','stale','mismatch','unmapped')`. Di dalam transaksi proses, baris diklaim lebih dulu (`INSERT … ON CONFLICT DO NOTHING`); kalah klaim berarti `duplicate` (dicatat sebagai baris tersendiri, `200`). Hasil akhir di-update di transaksi yang sama. Kalau transaksi gagal (`500`, misalnya reference tidak dikenal), klaimnya ikut batal sehingga retry PG diproses penuh, dan satu baris `error` dicatat di luar transaksi (best effort). Karena dedupe sekarang per connection, callback dari connection lain untuk reference yang sama boleh dicatat sebagai `mismatch` tanpa membuat callback asli terlihat duplikat; pengecualian `ConnectionMismatchError` di fase 3 tidak diperlukan lagi.
- **Lapisan Redis dihapus.** Dedupe cepat di Redis tidak menghemat apa pun lagi karena setiap callback toh dicatat ke database, jadi `Idempotencer` di `WebhookService` dibuang. `webhook_events` tidak ditulis lagi dan di-drop di migrasi contract. Callback yang sudah tercatat di `webhook_events` sebelum upgrade lalu diulang PG sesudahnya akan diproses lagi, tetapi state machine menjadikannya `stale`, jadi tidak ada forward ganda.
- **Status PG yang tidak dikenal** sekarang `unmapped`: adapter mengembalikan `InboundEvent.Status` kosong untuk status di luar tabelnya (sebelumnya Xendit memetakannya ke `PENDING`), sesuai properti 8.

**API admin.** `GET /admin/event-types`; `GET/POST /admin/apps/{id}/webhook-endpoints`; `GET/PATCH/DELETE /admin/webhook-endpoints/{id}` dan `rotate-secret`, `disable`, `enable`, `test` di bawahnya (operasi per endpoint tidak bersarang di app karena `whe_` sudah unik global); `GET /admin/webhook-deliveries` (cursor, filter `app_id`, `endpoint_id`, `event_id`, `event_type`, `status`, `environment`, `from`, `to`), `GET /admin/webhook-deliveries/{id}` (body + log attempt), replay dengan `dlv_`; `GET /admin/inbound-webhooks` (cursor, filter `connection_id`, `result`, `environment`, `from`, `to`) dan detailnya dengan raw body. Path inbound memakai `/admin/inbound-webhooks` (bukan `/admin/webhooks/inbound` dari sketsa) supaya sejajar dengan `/admin/webhook-deliveries`. List tidak membawa body; body hanya di detail.

**Otorisasi.** Endpoint adalah setting app: `apps:read` dan `apps:write`, termasuk katalog event. `deliveries:read` mencakup log keluar dan masuk, `deliveries:replay` untuk replay. Tidak ada permission baru.

Audit: `webhook_endpoint.create`, `.update` (hanya field yang berubah; header hanya nama), `.delete`, `.rotate_secret` (`after`: `secret_last4`, `previous_secret_expires_at`), `.disable`, `.enable`, `.auto_disable` (`system:worker`), `.test`, dan `webhook_delivery.replay` dengan target `dlv_…`. Snapshot endpoint berisi `id`, `app_id`, `url`, `description`, `events`, `api_version`, `legacy_signature`, `header_names`, `status`, `disabled_reason`, `retry_policy`, tanpa secret maupun nilai header.

**Konsol.** Tab Webhooks di detail app: daftar endpoint, tambah/edit (pemilih event dari katalog, header kustom, retry, versi body), secret reveal-once, rotasi dan hapus dengan konfirmasi mengetik host URL, kirim uji, dan status nonaktif otomatis beserta alasannya. Halaman Webhooks global: tab Outbound dan Inbound dengan filter chip, cursor, dan detail di panel samping. Karena semua path di bawah `/webhooks/` dicadangkan untuk callback PG, tab dan detail memakai query string (`/webhooks?tab=inbound&id=inb_…`), bukan path. Kedua tab difilter mode Test/Live lewat `environment` (join ke `apps.environment` dan `connections.environment`).

### Penyajian konsol

- Di production, server Go menyajikan build konsol (`handler.SPA`) untuk `GET`/`HEAD` yang tidak cocok dengan route API. File yang ada disajikan langsung; path lain mendapat `index.html` supaya reload dan deep link berfungsi.
- Path API yang tidak cocok mendapat `404 not_found` dalam envelope JSON, bukan `index.html`: `/v1`, `/admin`, `/healthz` beserta semua path di bawahnya, dan semua path di bawah `/webhooks/`. Method selain `GET`/`HEAD` di path konsol juga mendapat envelope JSON.
- `/webhooks` persis (tanpa segmen berikutnya) adalah halaman Webhooks di konsol, jadi `GET /webhooks` menyajikan `index.html`. Ini aman karena callback gateway selalu di bawah `/webhooks/<gateway>` (sekarang `/webhooks/xendit`, nanti `/webhooks/<connection>`), dan aturannya berbasis path, bukan header `Accept`, supaya respons tidak bergantung pada klien. Route konsol baru tidak boleh memakai `/v1`, `/admin`, `/healthz`, atau path di bawah `/webhooks/`.
- Di dev, proxy Vite meneruskan prefix yang sama ke backend dan melewatkan navigasi browser (`GET` dengan `Accept: text/html`) ke SPA.

### Uang dan waktu

- Uang: `int64` dalam minor unit + kode ISO 4217. Untuk IDR, minor unit yang dipakai KeiPay adalah rupiah utuh (eksponen 0), sesuai gateway; tampilan `Rp150.000`.
- Waktu disimpan `timestamptz` UTC dan dikirim RFC 3339; konsol menampilkan Asia/Jakarta (WIB).

### Port lokal dan gerbang kualitas

- Port bisa diatur lewat `.env` (`HTTP_PORT`, `UI_PORT`, `POSTGRES_PORT`, `REDIS_PORT`) atau per perintah (`make dev API_PORT=18080 UI_PORT=15173`). Vite membaca target proxy dari `VITE_API_TARGET`.
- `make ci` (`scripts/ci.sh`) adalah gerbang sebelum merge: gofmt, `go vet`, lint spec, cek hasil generate, migrasi up/down/up, `go test` dengan Postgres dan Redis asli, typecheck, test, dan build frontend, lalu smoke e2e Playwright terhadap server yang menyajikan build konsol. Postgres dan Redis jalan di project compose `keipay-ci` pada port 25432/26379 (API 28080) supaya tidak bentrok dengan stack dev, dan selalu dibongkar di akhir, juga saat gagal. CI di kemudian hari cukup memanggil target yang sama.

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

## Rencana fase

Setiap fase adalah satu branch dan satu PR yang bisa di-merge dan dijalankan sendiri. Fase 0–5 membuat KeiPay lengkap untuk satu gateway dengan konsol yang utuh; fase 6 ke atas menambah multi-gateway. Perubahan untuk app yang sudah ada dilakukan expand/contract, sehingga integrasi lama tetap jalan selama masa transisi.

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
| 8. Adapter DOKU | Jalur Checkout atau SNAP BI (lihat Open Questions), tanpa perubahan frontend | 6 | Skenario sandbox end-to-end lulus |
| 9. Health dan dashboard | Circuit breaker Redis, maintenance manual/terjadwal, `/admin/health`, dashboard | 6 | Transisi breaker dan fail-open teruji, diverifikasi di browser |

Di fase 3, kalau ada app yang dipakai untuk bisnis di luar pemilik akun Xendit, app itu harus dipindah ke connection milik pemiliknya sendiri. Jangan terus memakai connection default.

## Catatan hukum

- KeiPay tidak menampung atau menyalurkan dana, jadi deployment self-host oleh satu bisnis untuk aplikasinya sendiri tidak masuk wilayah izin PJP. README harus menyatakan dengan tegas bahwa memakai satu connection untuk menerima pembayaran bisnis lain (pemilik berbeda) **bukan** pemakaian yang didukung.
- Menjalankan KeiPay sebagai **layanan untuk merchant lain** berarti operatornya memegang API key dan data transaksi pihak ketiga. Uang tetap tidak lewat operator, tapi ada kewajiban UU PDP dan kemungkinan batasan ToS masing-masing PG soal pihak ketiga. Ini di luar cakupan v1 dan perlu kajian hukum sendiri.
- Dokumen ini bukan nasihat hukum.

## Open Questions

1. ~~Nama repo dan lisensi~~ Diputuskan: `keipay`, MIT.
2. DOKU: integrasi Checkout biasa atau jalur SNAP BI? Ini memengaruhi skema signature dan kredensial.
3. ~~Format `payment_id` publik~~ Diputuskan: `pay_<ULID>` di fase 2 (lihat [Konvensi](#konvensi)).
4. Perlu dukungan multi-currency di v1, atau IDR saja?
5. Batas waktu masa transisi header `X-Kei-Signature` lama.
