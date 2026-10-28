# KeiPay

Self-hosted payment orchestrator. Your apps call **one API with one credential**;
KeiPay creates the checkout on the payment gateway, receives the gateway's
webhook, and forwards a normalized, signed event to the app that asked for it.

```
App A ─┐                          ┌─► Xendit
App B ─┼─► KeiPay (1 credential) ─┼─► Midtrans   (planned)
App C ─┘        ▲                 └─► DOKU       (planned)
                │  gateway webhook
                └──► verify → normalize → sign → app webhook_url
```

> **Status: early.** Today KeiPay supports **Xendit Invoice** with one Xendit
> account per deployment. Multi-gateway connections, per-app routing
> (`auto`, weighted, fallback) and Midtrans/DOKU adapters are designed in
> [`docs/design.md`](docs/design.md) and not built yet.

## KeiPay never holds funds

Payments are created with **your own gateway credentials**, so money lands
directly in your gateway account. KeiPay only relays API calls and webhooks.

Use one gateway account only for apps owned by the business that owns that
account. Accepting payments for another business through your account and
paying them out yourself is a regulated activity in most jurisdictions
(in Indonesia it needs a Bank Indonesia payment-service licence) and breaks
most gateway terms of service. KeiPay is not built for, and does not support,
that use.

## Features

- App API `POST /v1/payments` with HMAC-SHA256 signing, timestamp window
  and Redis nonce replay protection.
- Apps with a `test` or `live` environment and any number of API keys
  (`kp_test_…` / `kp_live_…`), each with its own secret, optional expiry,
  last-used time, overlapping rotation and revocation.
- Idempotent payment creation per `order_id` and optional `Idempotency-Key`,
  with your own `metadata` echoed back in every webhook.
- Webhook ingestion that is idempotent (Redis fast path + durable DB guard in
  one transaction), applies status in order, and rejects callbacks whose
  amount or currency does not match the payment.
- At-least-once forwarding to each app's `webhook_url` with retries, a sweeper
  for stuck deliveries and admin replay.
- Per-app reporting of gross, fee and net amounts.
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
  internal/         domain, service, repository, transport, worker, xendit, pkg
  migrations/       SQL up/down
frontend/           React + Vite + Tailwind admin console (see DESIGN.md)
deploy/Dockerfile   one image: API + worker + console
compose.yaml        postgres, redis, migrate, keipay (+ asynqmon profile)
scripts/ci.sh       the local pre-merge gate behind `make ci`
Makefile            dev, build, test, gen, ci, docker targets
```

The backend follows a service-layer (ports-and-adapters) structure:
handlers stay thin, business rules live in `internal/service`, and
`internal/domain` has no external dependencies.

## Quick start (development)

Requirements: Go 1.26+, Node.js 22+, Docker, `openssl`.

```bash
make setup   # .env with generated secrets, deps, postgres + redis, migrate + seed
```

`make setup` prints the **demo app credentials** (a `test` app with one
API key, its secret and the webhook signing secret) once. Then set
`XENDIT_SECRET_KEY` and `XENDIT_CALLBACK_TOKEN` in `.env` (Xendit dashboard →
API keys / Webhooks) and run:

```bash
make dev     # API on :8080, console on :5173 (hot reload)
```

Open the console and follow [First sign-in](#first-sign-in).

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

In the Xendit dashboard, set the invoice webhook URL to
`https://<your-host>/webhooks/xendit` and copy its verification token into
`XENDIT_CALLBACK_TOKEN`.

## Running everything with Docker

```bash
cp .env.example .env    # or: make env
# set ENCRYPTION_KEY, XENDIT_SECRET_KEY, XENDIT_CALLBACK_TOKEN (and SETUP_TOKEN, see below)
docker compose up -d --build
```

This starts Postgres, Redis, a one-shot migration, and a single `keipay`
container serving the API, the webhook worker and the admin console on
`http://localhost:8080`. Optionally create a demo app (its credentials are
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
first: setup then asks for that value.

Owners add more users in **Settings → Users** and pick a role:

| Role | Can |
|---|---|
| `owner` | everything, including users |
| `admin` | everything except users |
| `viewer` | read apps, payments, ledger and webhook deliveries; no changes, no audit log |

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

Behind a reverse proxy, list the proxy in `TRUSTED_PROXIES` so sign-in rate
limits and the audit log see the real client IP. Session cookies are
`Secure` unless `APP_ENV=development`; serve the console over HTTPS, or set
`COOKIE_SECURE=false` for plain HTTP on a host other than `localhost`.

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
object.

## App API

Create an app in the console (Apps → Create app). You get three values,
shown once:

- **API key** (`kp_test_…` or `kp_live_…`): identifies the app and its
  environment; send it as `X-Api-Key`.
- **API secret** (`kps_test_…` / `kps_live_…`): signs your requests.
- **Webhook signing secret** (`kpwh_…`): verifies the webhooks KeiPay sends
  you.

An app can have several API keys (Apps → app → API keys). To rotate, create
the new key with **Rotate**, deploy it, then **Revoke** the old one; both
work in the meantime. Revoked and expired keys get `401 api_key_revoked` or
`401 api_key_expired`; a disabled app gets `403 tenant_inactive`.

Apps that existed before apps replaced tenants keep working unchanged: their
`kp_…` key and `api_secret` became the app's first key (environment `live`),
and that `api_secret` is also their webhook signing secret.

Every `/v1/*` request carries:

- `X-Api-Key`: the API key
- `X-Timestamp`: unix seconds (±5 min window)
- `X-Signature`: hex HMAC-SHA256 of `"{timestamp}.{rawBody}"` keyed with the API secret

### Create a payment

```bash
SECRET='kps_test_...'   # API secret
APIKEY='kp_test_...'    # API key
TS=$(date +%s)
BODY='{"order_id":"ord-123","amount":150000,"currency":"IDR","description":"Order 123","payer_email":"buyer@example.com","metadata":{"channel":"mobile"}}'
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

curl -s -X POST http://localhost:8080/v1/payments \
  -H "X-Api-Key: $APIKEY" -H "X-Timestamp: $TS" -H "X-Signature: $SIG" \
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

### Get a payment

`GET /v1/payments/:id`, signed the same way (empty body). `:id` is the
`pay_…` id or the legacy integer `payment_id`.

## Webhooks forwarded to your app

KeiPay POSTs to the app's `webhook_url` with:

- `X-Kei-Event`: e.g. `payment.paid`
- `X-Kei-Signature`: hex HMAC-SHA256 of the raw body keyed with the app's
  webhook signing secret (for apps from before apps replaced tenants: their
  original `api_secret`)

```json
{
  "event": "payment.paid",
  "payment_id": 1,
  "payment_public_id": "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
  "order_id": "ord-123",
  "external_id": "kp_pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
  "status": "PAID",
  "amount": 150000,
  "currency": "IDR",
  "metadata": { "channel": "mobile" },
  "paid_at": "2026-01-01T10:00:00Z",
  "timestamp": "2026-01-01T10:00:01Z"
}
```

Verify by recomputing the HMAC over the raw body and comparing in constant
time. Return 2xx to ack; anything else is retried.

Delivery is **at-least-once**: dedupe on `payment_public_id` + `event`. Status
only moves forward (`PENDING` → `PAID` → `SETTLED`); if callbacks arrive out
of order the stale one is dropped, so you may get `payment.settled` without
`payment.paid`. Treat settled as paid.

KeiPay matches gateway callbacks to payments by the stored reference
(`kp_pay_…`, or `{slug}:{order_id}` for payments created before apps), never
by parsing it.

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
- `GET /admin/payments` (cursor-paginated), `GET /admin/ledger`
- `GET /admin/webhook-deliveries`, `POST /admin/webhook-deliveries/:id/replay`
- `GET /admin/audit-logs`

Cursor-paginated lists take `limit` and `cursor` and return `next_cursor`.
Resources use prefixed ids (`app_…`, `key_…`, `pay_…`, `usr_…`).

The server checks each operation's permission against the caller's role
(`403 forbidden` otherwise); the table is in the spec and `GET /admin/me`
returns the caller's permissions.

Every admin change (users, apps, API keys, password changes, replays) and every
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

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
