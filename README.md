# KeiPay

Self-hosted payment orchestrator. Your apps call **one API with one
credential**; KeiPay creates the checkout on your payment gateway account,
receives the gateway's callback, and forwards a normalized, signed event to
the webhook endpoints of the app that created the payment. It also
reconciles its records with the gateway's.

> **Status: early.** Today KeiPay supports **Xendit Invoice**. Gateway
> accounts are configured in the console as connections (any number, each
> `test` or `live`), and each app uses one default connection. Routing an
> app across several connections (`auto`, weighted, fallback) and Midtrans
> and DOKU adapters are designed in [`docs/design.md`](docs/design.md) but
> not built yet.

**Contents:**
[How it works](#how-it-works) ·
[Features](#features) ·
[Quick start](#quick-start-development) ·
[Docker](#running-everything-with-docker) ·
[First sign-in](#first-sign-in) ·
[Connect a gateway](#connect-a-gateway-xendit) ·
[API contract](#api-contract) ·
[App API](#app-api) ·
[Webhooks sent to your app](#webhooks-sent-to-your-app) ·
[Reconciliation](#reconciliation) ·
[Admin API](#admin-api) ·
[Tests](#tests)

## KeiPay never holds funds

Payments are created with **your own gateway credentials**, so money lands
directly in your gateway account. KeiPay only relays API calls and webhooks.

Use one gateway account only for apps owned by the business that owns that
account. Accepting payments for another business through your account and
paying them out yourself is a regulated activity in most jurisdictions
(in Indonesia it needs a Bank Indonesia payment-service licence) and breaks
most gateway terms of service. KeiPay is not built for, and does not support,
that use.

## How it works

KeiPay is one Go process (API, background worker and admin console) backed
by Postgres and Redis. Each gateway account you add is a **connection**;
each app creates payments on its default connection.

```mermaid
flowchart LR
    app["Your app"]
    customer["Customer"]
    operator["Operator"]

    subgraph keipay["KeiPay"]
        v1["App API<br/>/v1/payments"]
        inbound["Gateway callbacks<br/>/webhooks/{provider}/{connection_id}"]
        console["Admin console<br/>and /admin API"]
        worker["Worker (asynq)<br/>deliveries, sweeper, reconciliation"]
    end

    pg[("Postgres<br/>apps, connections, payments,<br/>events, deliveries, reports")]
    redis[("Redis<br/>nonces, sessions, job queue")]
    gateway["Payment gateway<br/>(your Xendit account)"]
    endpoints["Your app's<br/>webhook endpoints"]

    app -- "signed request" --> v1
    v1 -- "create invoice" --> gateway
    customer -- "pays on checkout" --> gateway
    gateway -- "payment callback" --> inbound
    worker -- "signed event" --> endpoints
    worker -- "list transactions" --> gateway
    operator --> console
    v1 --- pg
    inbound --- pg
    console --- pg
    worker --- pg
    v1 --- redis
    console --- redis
    worker --- redis
```

Money moves only between the customer and your gateway account. KeiPay
stores what happened and tells your app about it.

### Creating a payment

Your app signs each request with its API secret. KeiPay checks the
signature, returns the original payment for a retried request, and
otherwise creates an invoice on the app's default connection. See
[App API](#app-api) for the details.

```mermaid
sequenceDiagram
    autonumber
    participant App as Your app
    participant API as KeiPay /v1
    participant Redis
    participant DB as Postgres
    participant GW as Gateway (Xendit)
    actor Customer

    App->>API: POST /v1/payments<br/>X-Api-Key, X-Timestamp, X-Signature, Idempotency-Key
    API->>API: X-Timestamp within 5 minutes?
    API->>DB: find the API key, its app and secret
    API->>API: HMAC-SHA256 of "{timestamp}.{rawBody}" matches?<br/>key not revoked or expired, app enabled?
    API->>Redis: claim the signature as a nonce (reuse is 401 replayed_request)
    API->>DB: payment with this Idempotency-Key or order_id?
    alt already created
        API-->>App: 201 original payment, Idempotent-Replayed true
    else new payment
        API->>DB: load the app's default connection and its channels
        API->>GW: create invoice, external_id kp_pay_…
        GW-->>API: invoice id and checkout URL
        API->>DB: insert payment (PENDING)
        API-->>App: 201 id, reference, invoice_url
    end
    App->>Customer: send to invoice_url
    Customer->>GW: pay on the gateway checkout
```

Without a default connection the request fails with
`422 no_eligible_connection`; if the gateway refuses or cannot be reached,
nothing is stored and the request can be retried with the same `order_id`.

### Gateway callbacks and webhook delivery

The gateway calls the URL of the connection the payment was made on. KeiPay
verifies the callback with that connection's credentials, applies it in one
database transaction, and delivers a signed event to every endpoint of the
app that subscribes to it.

```mermaid
sequenceDiagram
    autonumber
    participant GW as Gateway (Xendit)
    participant H as KeiPay /webhooks
    participant DB as Postgres
    participant Q as Queue (asynq on Redis)
    participant W as Worker
    participant E as Your webhook endpoints

    GW->>H: POST /webhooks/xendit/con_…<br/>(or the /webhooks/xendit alias)
    H->>H: verify x-callback-token with this connection's token
    alt verification or parsing fails
        H->>DB: record inbound webhook (rejected)
        H-->>GW: 401 or 400
    else verified
        Note over H,DB: one transaction
        H->>DB: claim the gateway event for this connection (duplicate stops here)
        H->>DB: lock the payment by its stored reference
        H->>DB: other connection, amount or currency? record as mismatch, do not apply
        H->>DB: status moves forward only? otherwise record as stale
        H->>DB: update the payment and write its payment report (gross, fee, net)
        H->>DB: insert the event and one pending delivery per subscribed active endpoint
        H->>Q: after commit, enqueue the first attempt of each delivery
        H-->>GW: 200
    end
    Q->>W: webhook:forward
    W->>E: POST event with X-KeiPay-Signature t=…,v1=…
    alt 2xx within 15 seconds
        W->>DB: delivery delivered
    else error, redirect or timeout
        W->>DB: log the attempt, schedule the next one (backoff)
        W->>Q: enqueue the next attempt
    end
    Note over W,E: After the last attempt the delivery fails. After WEBHOOK_DISABLE_AFTER_FAILURES<br/>failed deliveries in a row, the endpoint is disabled.
    Note over Q,DB: The sweeper re-enqueues pending deliveries whose attempt is overdue<br/>(for example when the enqueue after commit failed). An admin can replay a delivery.
```

A callback for a reference KeiPay does not know yet answers `500`, so the
gateway retries it. Every callback, accepted or not, is kept in the inbound
log, with two guards against anyone who learns a callback URL: bodies over
1 MiB are refused (`413 request_too_large`), and callbacks that fail
verification are limited per sender IP and per connection (by default 60
each in 15 minutes; `429 rate_limited` after that, without a log row).
Verified callbacks are never limited, whether KeiPay could parse them or
not, so the gateway's own traffic is always recorded whatever strangers
send to the same URL.

**Callbacks that never arrive.** A gateway may not send a callback, for
example when the "invoice expired" notification is off in its dashboard,
or the callback cannot reach KeiPay. The worker therefore reads back, once
a minute, the checkout of every pending payment that expired more than two
minutes ago (for up to three days, at most every ten minutes per payment).
A status other than pending is applied exactly like a callback, with the
same events, and the inbound log shows it with source `poll`. A callback
that arrives later is a duplicate. Providers that can read a checkout back
have the capability `read_checkout` (Xendit does).

### Payment status

Callbacks can arrive out of order, so a payment's status only moves
forward. A late success after `EXPIRED` or `FAILED` is still applied,
because the gateway captured the funds.

```mermaid
stateDiagram-v2
    [*] --> PENDING
    PENDING --> PAID
    PENDING --> SETTLED
    PENDING --> EXPIRED
    PENDING --> FAILED
    PAID --> SETTLED
    EXPIRED --> PAID
    EXPIRED --> SETTLED
    FAILED --> PAID
    FAILED --> SETTLED
    SETTLED --> [*]
```

Each change sends the matching event: `payment.paid`, `payment.settled`,
`payment.expired` or `payment.failed`.

### Reconciling with the gateway

An admin starts a run for one connection and date range. The worker pulls
the gateway's transactions, compares them by reference with KeiPay's
payments and their payment reports, and stores one item per reference.

```mermaid
sequenceDiagram
    autonumber
    actor Admin
    participant API as KeiPay /admin
    participant DB as Postgres
    participant W as Worker
    participant GW as Xendit Transactions API

    Admin->>API: POST /admin/reconciliation-runs<br/>connection, from, to (at most 31 days)
    API->>DB: insert run (queued) and audit log entry
    API->>W: enqueue reconciliation:run
    W->>DB: claim the run (running)
    W->>GW: GET /transactions, type PAYMENT, page by page<br/>range widened by one hour on each side
    W->>DB: load the connection's payments and payment reports
    W->>W: match by reference
    W->>DB: store items and summary (succeeded)
    Admin->>API: list items, resolve differences with a note, export CSV
```

```mermaid
flowchart TD
    start(["One reference"]) --> inKeipay{"Payment in KeiPay?"}
    inKeipay -- no --> gwPaid{"Paid at the gateway?"}
    gwPaid -- yes --> missingKeipay["missing_in_keipay"]
    gwPaid -- no --> skip["not listed"]
    inKeipay -- yes --> inGateway{"Transaction at the gateway?"}
    inGateway -- no --> kpPaid{"Paid in KeiPay?"}
    kpPaid -- yes --> missingGateway["missing_in_gateway"]
    kpPaid -- no --> skip
    inGateway -- yes --> amount{"Same amount and currency?"}
    amount -- no --> amountMismatch["amount_mismatch"]
    amount -- yes --> status{"Paid on both sides,<br/>or on neither?"}
    status -- no --> statusMismatch["status_mismatch"]
    status -- yes --> fee{"Paid, both fees known<br/>and different?"}
    fee -- yes --> feeMismatch["fee_mismatch"]
    fee -- no --> matched["matched"]
```

"Paid" means `PAID` or `SETTLED`.

## Features

- App API `POST /v1/payments` with HMAC-SHA256 signing, timestamp window
  and Redis nonce replay protection.
- Apps with a `test` or `live` environment and any number of API keys
  (`kp_test_…` / `kp_live_…`), each with its own secret, optional expiry,
  last-used time, overlapping rotation and revocation.
- Idempotent payment creation per `order_id` and optional `Idempotency-Key`,
  with your own `metadata` echoed back in every webhook.
- Gateway connections managed in the console: credentials validated
  against the gateway adapter's schema, envelope-encrypted at rest and never
  shown again (last 4 characters only), a connection test, a callback URL
  per connection verified with that connection's own secret, and the
  payment channels each connection offers.
- Webhook ingestion that is idempotent per connection (a durable guard in
  the same transaction as the status change), applies status in order,
  rejects callbacks whose amount or currency does not match the payment, and
  keeps every callback (raw body, verification and outcome) for inspection.
- A webhook router: several endpoints per app, each with its own event
  subscriptions from a served event catalog, signing secret (rotation with
  an overlap window), custom headers and retry policy; a versioned event
  envelope signed with a timestamp; at-least-once delivery with a log of
  every attempt, replay, and automatic disabling of endpoints that keep
  failing.
- Reconciliation per connection and period: KeiPay pulls the gateway's
  transactions, matches them with its own payments by reference, and lists
  every difference (missing on either side, amount, status or fee
  mismatch) for an admin to resolve with a note. Payment reports (gross,
  fee, net) are totalled per app, connection, currency and day or month,
  and both reports and run results export as streamed CSV.
- Admin console (React) served by the same process: console users with
  `owner`, `admin` and `viewer` roles, cookie sessions stored in Redis, CSRF
  protection, rate-limited sign-in, and an audit log of every admin change
  and sign-in.
- API and webhook secrets encrypted at rest with AES-256-GCM; API keys
  stored only as SHA-256 hashes.

## Repository layout

```
api/openapi.yaml    HTTP contract (source of truth; frontend types are generated from it)
backend/            Go module github.com/mydisha/keipay/backend
  cmd/server/       API + embedded asynq worker (+ serves the console)
  cmd/migrate/      migrations, -seed, -reset-admin
  internal/         domain, service, provider (gateway adapters), repository, transport, worker, pkg
  migrations/       SQL up/down
frontend/           React + Vite + Tailwind admin console (see DESIGN.md)
deploy/Dockerfile   one image: API + worker + console
compose.yaml        postgres, redis, migrate, keipay (+ asynqmon profile)
scripts/ci.sh       the local pre-merge gate behind `make ci`
Makefile            dev, build, test, gen, ci, docker targets
```

The backend follows a service-layer (ports-and-adapters) structure:
handlers stay thin, business rules live in `internal/service`, and
`internal/domain` has no external dependencies. See
[docs/architecture.md](docs/architecture.md) for a full map of the backend
and the console, how a request flows through the code, and where to start
for common changes.

## Quick start (development)

Requirements: Go 1.26+, Node.js 22+, Docker, `openssl`.

```bash
make setup   # .env with generated secrets, deps, postgres + redis, migrate + seed
```

`make setup` prints the **demo app credentials** (a `test` app with one
API key and its secret) once. Then run:

```bash
make dev     # API on :8080, console on :5173 (hot reload)
```

Open the console, follow [First sign-in](#first-sign-in), then
[connect Xendit](#connect-a-gateway-xendit).

Other targets: `make backend`, `make frontend`, `make test`, `make check`,
`make ci`, `make gen`, `make reset-admin EMAIL=... PW=...`, `make help`.

### Ports

Ports come from `.env` (`HTTP_PORT`, `UI_PORT`, `POSTGRES_PORT`,
`REDIS_PORT`). If the defaults are taken on your machine, choose others when
you create `.env`, and `make setup` writes them consistently (including
`POSTGRES_DSN` and `REDIS_ADDR`):

```bash
make setup API_PORT=18080 UI_PORT=15173 POSTGRES_PORT=15432 REDIS_PORT=16379
make dev     # API on :18080, console on :15173
```

Any target also accepts a one-off override, e.g. `make dev API_PORT=18080`.

## Running everything with Docker

```bash
cp .env.example .env    # or: make env
# set ENCRYPTION_KEY and HTTP_BASE_URL (and SETUP_TOKEN, see below)
docker compose up -d --build
```

This starts Postgres, Redis, a one-shot migration, and a single `keipay`
container serving the API, the background worker and the admin console on
`http://localhost:8080` (host port `KEIPAY_PORT`). Optionally create a demo app (its credentials are
printed once):

```bash
docker compose run --rm --entrypoint /app/keipay-migrate keipay -seed
```

Then open `http://localhost:8080` and follow [First sign-in](#first-sign-in).

Optional queue UI: `docker compose --profile tools up -d asynqmon` (`:8081`).

## First sign-in

While no console user exists, the console opens a **first-run setup** page
that creates the first **owner** (email, name, password of at least 12
characters) and signs you in. Setup works exactly once; after that the page
redirects to sign-in.

Anyone who reaches the console before you can run setup. If the console is
reachable by others before you create the owner, set `SETUP_TOKEN` in `.env`
first: setup then asks for that value. With `APP_ENV=production` (the
Docker default) the token is required: setup answers
`503 setup_token_not_configured` until it is set, and an install that is
already set up is unaffected.

Owners add more users in **Settings → Users** and pick a role:

| Role | Can |
|---|---|
| `owner` | everything, including users |
| `admin` | everything except users |
| `viewer` | read apps (including webhook endpoints), connections, payments, payment reports, reconciliation runs and webhook logs; no changes, no audit log |

New users and password resets get a temporary password, shown once, that
must be changed at first sign-in.

**Locked out?** On the server, set a password for a user by email (a random
one is generated and printed if `PW` is empty, and must then be changed at
sign-in). It also re-enables that user and ends their sessions. With no users
at all, it creates the owner instead of running setup:

```bash
make reset-admin EMAIL=you@example.com PW='a long passphrase'
# Docker:
docker compose run --rm --entrypoint /app/keipay-migrate keipay -reset-admin -email you@example.com
```

**Upgrading from a version with the single `admin` login:** that account
becomes an owner with the email `admin@keipay.invalid` and keeps its
password. Sign in with that email, then change it in Settings → Users.

Behind a reverse proxy, list the proxy in `TRUSTED_PROXIES` so the sign-in,
`/v1` and callback limits and the audit log see the real client IP; with it
empty every caller shares the proxy's address and the per-IP limits apply
to all of them at once (production warns about this at startup). Session
cookies are
`Secure` unless `APP_ENV=development`; serve the console over HTTPS, or set
`COOKIE_SECURE=false` for plain HTTP on a host other than `localhost`
(refused in production). Every response carries the usual browser
protections (`nosniff`, no framing, no referrer, a content security policy
and `Cache-Control: no-store`), and a path with a stray trailing slash is a
JSON 404 rather than a redirect; `Strict-Transport-Security` is sent
whenever cookies are `Secure`, or as `HSTS_ENABLED` says. In production
the server also warns at startup when Redis has no password or the
Postgres DSN has `sslmode=disable`.

Request bodies are capped: 64 KiB on `/v1`, 1 MiB on `/admin` and on
gateway callbacks (`413 request_too_large`). A client `X-Request-ID` is
echoed and logged only when it is at most 128 characters of letters,
digits, `.`, `_` and `-`; otherwise the server generates one.

## Connect a gateway (Xendit)

Gateway accounts are **connections**, managed in the console (owner and
admin; viewers can look). Set `HTTP_BASE_URL` to the public URL the gateway
can reach first: callback URLs are built from it.

1. **Connections → Add connection.** Pick Xendit, the environment (`test`
   for a Xendit development key `xnd_development_…`, `live` for
   `xnd_production_…`) and a name such as `xendit-live`.
2. Paste the **secret API key** (Xendit dashboard → Settings → API keys;
   a restricted key with Invoice read and write is enough for payments,
   and [reconciliation](#reconciliation) also needs read access to
   transactions) and the
   **webhook verification token** (Settings → Webhooks). KeiPay encrypts
   them and from then on shows only their last 4 characters.
3. KeiPay **tests** the credentials right away. Fix and retest if needed.
4. Copy the connection's **callback URL**,
   `https://<your-host>/webhooks/xendit/con_…`, into Xendit → Settings →
   Webhooks → Invoices paid. Each connection has its own URL and is
   verified with its own token. Also turn on the notification for
   expired invoices, so expiries arrive right away rather than through
   the read-back.
5. Choose the **channels** to offer. Leave all on to offer everything your
   Xendit account has activated.
6. Open each app (**Apps → app → Overview**) and pick its **default
   connection**. It must have the app's environment. Payments of an app
   without one fail with `422 no_eligible_connection`.

To change keys later, use **Replace credentials** on the same connection;
do not add a second connection for the same Xendit account, because Xendit
sends callbacks to one URL only. A connection that is some app's default
cannot be disabled until the apps use another one.

**Upgrading from `XENDIT_SECRET_KEY` / `XENDIT_CALLBACK_TOKEN`:** keep both
set for the first start after upgrading. The server imports them once into
a connection named `xendit-imported` (environment from the key prefix),
attaches existing payments to it and makes it the default connection of
every app with the same environment. `/webhooks/xendit` keeps working as an
alias for that connection, so nothing breaks; then move the Xendit dashboard
to the connection's own URL and remove the two variables (the server warns
while they are set). Apps whose environment differs from the key's (for
example `live` apps with a development key) get no default connection and
are listed in the server log.

## API contract

[`api/openapi.yaml`](api/openapi.yaml) (OpenAPI 3.1) describes every endpoint,
including the webhook KeiPay sends to your app.

### Errors

Every error has the same shape on every endpoint:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "invalid request body",
    "details": { "fields": [{ "field": "order_id", "rule": "required" }] }
  }
}
```

Branch on `code`; it is stable and listed under `ErrorCode` in the spec
(for example `invalid_signature`, `timestamp_out_of_range`, `conflict`,
`not_found`). `message` is for humans and may change. `details` is always an
object. Three codes are worth handling in every client: `429 rate_limited`
(wait `Retry-After` seconds), `413 request_too_large`, and
`503 auth_unavailable` on `/v1` (nothing was processed; retry later with
the same `Idempotency-Key` or `order_id`).

## App API

Create an app in the console (Apps → Create app). You get two values,
shown once:

- **API key** (`kp_test_…` or `kp_live_…`): identifies the app and its
  environment; send it as `X-Api-Key`.
- **API secret** (`kps_test_…` / `kps_live_…`): signs your requests.

To receive events, add a webhook endpoint (Apps → app → Webhooks); see
[Webhooks sent to your app](#webhooks-sent-to-your-app).

An app can have several API keys (Apps → app → API keys). To rotate, create
the new key with **Rotate**, deploy it, then **Revoke** the old one; both
work in the meantime. Revoked and expired keys get `401 api_key_revoked` or
`401 api_key_expired`; a disabled app gets `403 tenant_inactive`.

Apps that existed before apps replaced tenants keep working unchanged: their
`kp_…` key and `api_secret` became the app's first key (environment `live`).

Every `/v1/*` request carries:

- `X-Api-Key`: the API key
- `X-Timestamp`: unix seconds (±5 min window)
- `X-Signature-Version`: `2`
- `X-Nonce`: a value you generate per request (16-64 printable ASCII
  characters without spaces; a UUID is fine)
- `X-Signature`: hex HMAC-SHA256 of
  `"{timestamp}.{nonce}.{METHOD}.{path}.{rawBody}"` keyed with the API
  secret. `METHOD` is upper case and `path` is the request path without the
  query string, for example `POST` and `/v1/payments`, or `GET` and
  `/v1/payments/pay_01JB…`. The body of a `GET` is empty, so its signed
  input ends with the `.` after the path.

Each nonce is accepted once: KeiPay remembers it for 15 minutes, and a
request that reuses one gets `401 replayed_request`. To retry, send a new
nonce and timestamp (and keep the same `order_id` or `Idempotency-Key`, so
the retry returns the original payment). The replay check never fails open:
if the store behind it is down you get `503 auth_unavailable`, nothing was
processed, and the same retry applies.

Without `X-Signature-Version`, KeiPay falls back to the original scheme
(version 1): `X-Signature` is the HMAC of `"{timestamp}.{rawBody}"` and the
signature itself is the nonce, so two identical requests in the same second
collide. Existing integrations keep working; new ones should send version 2,
which also ties a signature to its method and path.

Bodies over 64 KiB are refused with `413 request_too_large`. Failed
authentications are limited: unknown keys count against your IP, bad
signatures and replays against your IP and against your IP together with
the key (by default 100 per IP and 50 per IP and key in 15 minutes), and
over the limit every request from that address gets `429 rate_limited`
with `Retry-After` until the window ends. Successful requests never count,
and the key counter is per sender, so nobody who only knows your key can
lock you out.

### Create a payment

```bash
SECRET='kps_test_...'   # API secret
APIKEY='kp_test_...'    # API key
TS=$(date +%s)
NONCE=$(uuidgen)
BODY='{"order_id":"ord-123","amount":150000,"currency":"IDR","description":"Order 123","payer_email":"buyer@example.com","metadata":{"channel":"mobile"}}'
SIG=$(printf '%s.%s.POST./v1/payments.%s' "$TS" "$NONCE" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

curl -s -X POST http://localhost:8080/v1/payments \
  -H "X-Api-Key: $APIKEY" -H "X-Timestamp: $TS" \
  -H "X-Signature-Version: 2" -H "X-Nonce: $NONCE" -H "X-Signature: $SIG" \
  -H "Idempotency-Key: 3f1c2d9e-7b1a-4c55-9a1e-2f0f6f1b9c11" \
  -H "Content-Type: application/json" -d "$BODY"
```

```json
{
  "id": "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
  "payment_id": 1,
  "reference": "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
  "order_id": "ord-123",
  "status": "PENDING",
  "amount": 150000,
  "currency": "IDR",
  "invoice_url": "https://checkout.xendit.co/web/...",
  "metadata": { "channel": "mobile" }
}
```

Creation is idempotent per `order_id` within your app: a retry with the same
amount and currency returns the original payment; a different amount or
currency returns `409 conflict`. With `Idempotency-Key`, the same key and the
same request return the original payment, and the same key with a different
request returns `409 idempotency_key_reused`. Replays answer `201` with
`Idempotent-Replayed: true`. An expired payment is returned as-is, so use a
new `order_id` to retry.

`metadata` is a flat object (at most 50 keys; values are strings up to 500
characters, numbers, booleans or `null`; 8 KB in total). It is stored with
the payment and sent back in every webhook for it.

`payment_id` (integer) is kept for existing integrations; use `id`
(`pay_…`) in new code.

The checkout is created on the app's default connection. Without one the
request fails with `422 no_eligible_connection`. When the gateway refuses
the request it fails with `422 provider_rejected`; when the gateway cannot
be reached or has a server error, `503 provider_unavailable`. No payment is
stored in either case, so retry with the same `order_id`.

### Get a payment

`GET /v1/payments/:id`, signed the same way with an empty body, so the
signed input is `"{timestamp}.{nonce}.GET./v1/payments/{id}."`. `:id` is the
`pay_…` id or the legacy integer `payment_id`.

## Webhooks sent to your app

Add one or more **webhook endpoints** to an app (Apps → app → Webhooks, or
`POST /admin/apps/:id/webhook-endpoints`). Each endpoint has a URL, the
events it subscribes to, its own signing secret (`kpwh_…`, shown once),
optional custom headers and a retry policy. Pick events from the catalog
(`GET /admin/event-types`): an exact type such as `payment.paid`, a group
such as `payment.*`, or `*` for everything, including types added later.

| Event | Sent when |
|---|---|
| `payment.paid` | The payer paid. |
| `payment.settled` | The gateway settled the funds. |
| `payment.expired` | The checkout expired unpaid. |
| `payment.failed` | The payment failed. |
| `payment.duplicate` | Reserved for a payment paid more than once; not sent yet. |

Every request is a `POST` with `Content-Type: application/json` and:

- `X-KeiPay-Event-Id`: the event id (`evt_…`), the same for every endpoint
  and every retry. **Dedupe on it.**
- `X-KeiPay-Event-Type`: e.g. `payment.paid`
- `X-KeiPay-Delivery-Id`: this endpoint's delivery of the event (`dlv_…`)
- `X-KeiPay-Signature`: `t=<unix seconds>,v1=<hex>`
- `X-KeiPay-Test: true` on test sends from the console

The body (`api_version` `v2`) is a versioned envelope. Fields are only ever
added within a version.

```json
{
  "id": "evt_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
  "type": "payment.paid",
  "api_version": "v2",
  "created_at": "2026-01-01T10:00:01Z",
  "data": {
    "id": "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    "order_id": "ord-123",
    "reference": "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    "status": "PAID",
    "amount": 150000,
    "currency": "IDR",
    "metadata": { "channel": "mobile" },
    "provider": "xendit",
    "connection_id": "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    "paid_at": "2026-01-01T10:00:00Z",
    "settled_at": null
  }
}
```

### Verify the signature

`v1` is the hex HMAC-SHA256 of `"{t}.{raw body}"` keyed with the endpoint's
signing secret. Verify against the **raw** body before parsing it, compare in
constant time, and reject a `t` more than 5 minutes from your clock. Right
after a secret rotation the header carries one `v1=` per valid secret
(`t=…,v1=<new>,v1=<old>`); accept the request when any of them matches, so
both your old and new secret work during the overlap window.

Go:

```go
import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// VerifyKeiPaySignature reports whether header, the X-KeiPay-Signature
// value, signs body with secret and is at most tolerance away from now.
func VerifyKeiPaySignature(header string, body []byte, secret string, tolerance time.Duration, now time.Time) bool {
	var ts string
	var sigs []string
	for _, part := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if !ok {
			continue
		}
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return false
	}
	if d := now.Sub(time.Unix(t, 0)); d > tolerance || d < -tolerance {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	for _, sig := range sigs {
		if hmac.Equal([]byte(sig), []byte(want)) {
			return true
		}
	}
	return false
}
```

Node.js (`rawBody` is the unparsed body as a `Buffer`):

```js
import crypto from "node:crypto";

export function verifyKeiPaySignature(header, rawBody, secret, toleranceSeconds = 300) {
  const parts = (header ?? "").split(",").map((part) => part.trim().split("="));
  const t = parts.find(([key]) => key === "t")?.[1];
  const sigs = parts.filter(([key]) => key === "v1").map(([, value]) => value ?? "");
  if (!/^\d+$/.test(t ?? "") || Math.abs(Date.now() / 1000 - Number(t)) > toleranceSeconds) return false;
  const want = Buffer.from(crypto.createHmac("sha256", secret).update(`${t}.`).update(rawBody).digest("hex"));
  return sigs.some((sig) => {
    // Compare byte lengths, not string lengths: timingSafeEqual throws on
    // buffers of different sizes, and non-ASCII characters take several bytes.
    const got = Buffer.from(sig);
    return got.length === want.length && crypto.timingSafeEqual(got, want);
  });
}
```

### Delivery, retries and replay

Return any `2xx` within 15 seconds to acknowledge. Anything else (including
a redirect or a timeout) is retried with the endpoint's retry policy (default
8 attempts with exponential backoff from 30 seconds, about an hour in all).
Delivery is **at-least-once**, so dedupe on the event id. Status only moves
forward (see [Payment status](#payment-status)); if gateway callbacks arrive
out of order the stale one is dropped, so you may get `payment.settled`
without `payment.paid`. Treat settled as paid.

After `WEBHOOK_DISABLE_AFTER_FAILURES` (default 5; `0` never disables)
deliveries in a row that ran out of attempts, KeiPay disables the endpoint
and shows why in the console. A successful delivery resets the count. Fix
the receiver, enable the endpoint, then replay the failed deliveries
(Webhooks → Outbound). A replay resends the same event id and body once.

If queuing an attempt fails, the delivery stays `pending` in Postgres and a
sweeper, running every minute in the worker, queues it again once it is
more than two minutes overdue.

Every gateway callback KeiPay receives is kept with its raw body, whether
verification passed and what happened to it (Webhooks → Inbound). A callback
only updates payments created through the connection whose URL it arrived
at, and is matched by the stored reference (`kp_pay_…`, or
`{slug}:{order_id}` for payments created before apps), never by parsing it.
Bodies that failed verification are stored cut to 4 KiB, and verified
bodies that could not be parsed cut to 64 KiB (`body_truncated` says so);
applied callbacks are kept whole.

### Upgrading from an app `webhook_url`

Apps used to have a single `webhook_url`. On upgrade, each app with one gets
a webhook endpoint for that URL that subscribes to `*`, keeps the app's old
signing secret, and is marked migrated. Nothing changes for the receiver: it
gets the same flat body as before (`api_version` `v1`, now with an
`event_id` field) and the same `X-Kei-Signature` (hex HMAC-SHA256 of the raw
body) and `X-Kei-Event` headers, next to the new `X-KeiPay-*` headers. To
move on, verify `X-KeiPay-Signature`, then switch the endpoint to `v2` and
turn off the legacy signature in the console. `X-Kei-Signature` is always
signed with the current secret only, so stop relying on it before you rotate
the secret.

`WEBHOOK_MAX_RETRY` is no longer read; retries are set per endpoint.

## Reconciliation

KeiPay never holds funds, so reconciling means checking that KeiPay and the
gateway agree about every payment (see the
[diagrams](#reconciling-with-the-gateway) above). In the console, open
**Reconciliation**:

- **Summary** totals the payment reports (one per paid payment: gross, fee,
  net) per app, connection and currency for a date range, as one total or
  per day or month. Dates are Asia/Jakarta (WIB); times are stored in UTC.
  **Export CSV** downloads the matching reports.
- **Runs** compare one connection over a date range of at most 31 days.
  The background worker (`RUN_WORKER=true`, the default) pulls the
  gateway's transactions (today: Xendit's Transactions API, which needs
  the secret key's read permission for transactions) and matches them with
  KeiPay's payments by reference. Each reference becomes an item:
  `matched`, `missing_in_keipay`, `missing_in_gateway`, `amount_mismatch`,
  `status_mismatch` or `fee_mismatch`. Resolve the others with a note
  once you have looked at them; this is recorded in the audit log. Items
  of a run whose last re-run failed cannot be resolved until it succeeds.
  **Export CSV** downloads a run's items. **Re-run** recomputes the same
  run without duplicating items, and an item keeps its resolution while its
  result stays the same.

**Fees.** KeiPay records the gateway fee from the payment callback: the
paid amount minus Xendit's `adjusted_received_amount` (what you receive).
When the callback does not carry that field, the fee and net are unknown,
and the run does not compare fees for that payment. On the gateway side the
fee is Xendit's fee plus its VAT.

Without a worker, runs stay `queued`. At most two runs work at a time, on
workers of their own, so a long run never delays webhook forwarding; the
rest wait in their queue. A run that is still queued or running after 15
minutes without progress can be re-run.

## Admin API

The admin API is for the console. `POST /admin/login` (or `POST /admin/setup`
on first run) sets an `HttpOnly` session cookie, `keipay_session`, and a
CSRF cookie, `keipay_csrf`. Every `POST`/`PUT`/`PATCH`/`DELETE` under
`/admin/` must echo the `keipay_csrf` value in the `X-CSRF-Token` header.
Sessions expire after 30 minutes idle or 12 hours in total
(`SESSION_IDLE_TIMEOUT`, `SESSION_ABSOLUTE_TIMEOUT`).

- `GET/POST /admin/setup`, `POST /admin/login`, `POST /admin/logout`
- `GET /admin/me`, `POST /admin/me/password`
- `GET/POST /admin/users`, `GET/PATCH /admin/users/:id`,
  `POST /admin/users/:id/disable|enable|reset-password` (owner only)
- `GET/POST /admin/apps`, `GET/PATCH /admin/apps/:id`,
  `POST /admin/apps/:id/disable|enable`
- `GET/POST /admin/apps/:id/keys`, `POST /admin/apps/:id/keys/:key_id/rotate|revoke`
- `GET /admin/providers`
- `GET/POST /admin/connections`, `GET/PATCH /admin/connections/:id`,
  `PATCH /admin/connections/:id/credentials`, `PUT /admin/connections/:id/channels`,
  `POST /admin/connections/:id/test|disable|enable`
- `GET /admin/payments` (cursor-paginated)
- `GET /admin/payment-reports` (cursor-paginated), `GET /admin/payment-reports/summary`,
  `GET /admin/payment-reports/export` (CSV)
- `GET/POST /admin/reconciliation-runs`, `GET /admin/reconciliation-runs/:id`,
  `POST /admin/reconciliation-runs/:id/rerun`, `GET /admin/reconciliation-runs/:id/items`,
  `GET /admin/reconciliation-runs/:id/items/export` (CSV),
  `GET /admin/reconciliation-items/:id`, `POST /admin/reconciliation-items/:id/resolve`
- `GET /admin/event-types`
- `GET/POST /admin/apps/:id/webhook-endpoints`,
  `GET/PATCH/DELETE /admin/webhook-endpoints/:id`,
  `POST /admin/webhook-endpoints/:id/rotate-secret|disable|enable|test`
- `GET /admin/webhook-deliveries` (cursor-paginated), `GET /admin/webhook-deliveries/:id`,
  `POST /admin/webhook-deliveries/:id/replay`
- `GET /admin/inbound-webhooks` (cursor-paginated), `GET /admin/inbound-webhooks/:id`
- `GET /admin/audit-logs`

Cursor-paginated lists take `limit` and `cursor` and return `next_cursor`.
Resources use prefixed ids (`app_…`, `key_…`, `pay_…`, `con_…`, `whe_…`,
`evt_…`, `dlv_…`, `inb_…`, `rec_…`, `rci_…`, `usr_…`).

The server checks each operation's permission against the caller's role
(`403 forbidden` otherwise); the table is in the spec and `GET /admin/me`
returns the caller's permissions.

Every admin change (users, apps, API keys, connections, webhook endpoints, password changes, replays, reconciliation runs and resolved items) and every
sign-in, failed sign-in and sign-out is recorded in the audit log with who,
what, when and before/after values, never secrets or passwords.

## Tests

```bash
make check   # fast: gofmt, vet, unit tests, API lint, typecheck, frontend tests
make ci      # full gate (needs Docker): adds Postgres/Redis integration tests,
             # migration round-trip, generated-type drift check, frontend
             # build and a Playwright smoke test
```

`make ci` starts Postgres and Redis in a throwaway compose project
(`keipay-ci`, host ports 25432 and 26379, API on 28080; override with
`CI_POSTGRES_PORT`, `CI_REDIS_PORT`, `CI_API_PORT`) and always removes it at
the end. `make e2e` runs only the Playwright smoke test.

Postgres integration tests run only when `KEI_TEST_POSTGRES_DSN` points at a
migrated database:

```bash
KEI_TEST_POSTGRES_DSN='postgres://keipay:keipay@localhost:5432/keipay?sslmode=disable' \
  go test ./backend/internal/repository/postgres/ -run Integration
```

Tests that need an empty `users` table (migrations, the first-run setup race,
the `keipay-migrate` CLI) create a throwaway database on that server and drop
it afterwards, so the DSN's role needs the `CREATEDB` privilege
(`ALTER ROLE keipay CREATEDB`). The `make ci` stack already uses a superuser.

### Against the Xendit sandbox

To see real callbacks locally, give the API a public HTTPS URL with a
tunnel, set `HTTP_BASE_URL` to it and paste the connection's callback URL
into the Xendit dashboard (test mode). Use a tunnel with a fixed domain,
such as an ngrok static domain: random quick-tunnel domains can change
while you wait for a callback, and the Xendit dashboard refused a
`trycloudflare.com` URL. In the test-mode checkout, each payment method
has a button that simulates the payment.

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
