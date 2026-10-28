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

- Tenant API `POST /v1/payments` with HMAC-SHA256 signing, timestamp window
  and Redis nonce replay protection.
- Idempotent payment creation per `order_id`.
- Webhook ingestion that is idempotent (Redis fast path + durable DB guard in
  one transaction), applies status in order, and rejects callbacks whose
  amount or currency does not match the payment.
- At-least-once forwarding to each app's `webhook_url` with retries, a sweeper
  for stuck deliveries and admin replay.
- Per-app reporting of gross, fee and net amounts.
- Admin console (React) served by the same process, with an audit log of
  every admin change.
- Tenant secrets encrypted at rest with AES-256-GCM.

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

`make setup` prints the **admin password** and the **demo tenant
credentials** once. Then set `XENDIT_SECRET_KEY` and `XENDIT_CALLBACK_TOKEN`
in `.env` (Xendit dashboard → API keys / Webhooks) and run:

```bash
make dev     # API on :8080, console on :5173 (hot reload)
```

Other targets: `make backend`, `make frontend`, `make test`, `make check`,
`make ci`, `make gen`, `make reset-admin PW=...`, `make help`.

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
# set JWT_SECRET, ENCRYPTION_KEY, XENDIT_SECRET_KEY, XENDIT_CALLBACK_TOKEN
docker compose up -d --build
```

This starts Postgres, Redis, a one-shot migration, and a single `keipay`
container serving the API, the webhook worker and the admin console on
`http://localhost:8080`. Create the admin user and a demo tenant once
(credentials are printed once):

```bash
docker compose run --rm --entrypoint /app/keipay-migrate keipay -seed
```

Optional queue UI: `docker compose --profile tools up -d asynqmon` (`:8081`).

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

## Tenant API

Every `/v1/*` request carries:

- `X-Api-Key`: tenant `api_key`
- `X-Timestamp`: unix seconds (±5 min window)
- `X-Signature`: hex HMAC-SHA256 of `"{timestamp}.{rawBody}"` keyed with the tenant `api_secret`

### Create a payment

```bash
SECRET='sk_...'   # tenant api_secret
APIKEY='kp_...'   # tenant api_key
TS=$(date +%s)
BODY='{"order_id":"ord-123","amount":150000,"currency":"IDR","description":"Order 123","payer_email":"buyer@example.com"}'
SIG=$(printf '%s.%s' "$TS" "$BODY" | openssl dgst -sha256 -hmac "$SECRET" | awk '{print $2}')

curl -s -X POST http://localhost:8080/v1/payments \
  -H "X-Api-Key: $APIKEY" -H "X-Timestamp: $TS" -H "X-Signature: $SIG" \
  -H "Content-Type: application/json" -d "$BODY"
```

```json
{
  "payment_id": 1,
  "order_id": "ord-123",
  "status": "PENDING",
  "amount": 150000,
  "currency": "IDR",
  "invoice_url": "https://checkout.xendit.co/web/..."
}
```

Creation is idempotent per `order_id`: a retry with the same amount and
currency returns the original payment; a different amount or currency returns
`409`. An expired payment is returned as-is, so use a new `order_id` to retry.

### Get a payment

`GET /v1/payments/:id`, signed the same way (empty body).

## Webhooks forwarded to your app

KeiPay POSTs to the tenant `webhook_url` with:

- `X-Kei-Event`: e.g. `payment.paid`
- `X-Kei-Signature`: hex HMAC-SHA256 of the raw body keyed with the tenant `api_secret`

```json
{
  "event": "payment.paid",
  "payment_id": 1,
  "order_id": "ord-123",
  "external_id": "demo:ord-123",
  "status": "PAID",
  "amount": 150000,
  "currency": "IDR",
  "paid_at": "2026-01-01T10:00:00Z",
  "timestamp": "2026-01-01T10:00:01Z"
}
```

Verify by recomputing the HMAC over the raw body and comparing in constant
time. Return 2xx to ack; anything else is retried.

Delivery is **at-least-once**: dedupe on `payment_id` + `event`. Status only
moves forward (`PENDING` → `PAID` → `SETTLED`); if callbacks arrive out of order
the stale one is dropped, so you may get `payment.settled` without
`payment.paid`. Treat settled as paid.

## Admin API

`POST /admin/login` returns a JWT; send it as `Authorization: Bearer <token>`:

- `POST/GET /admin/tenants`, `PUT /admin/tenants/:id`, `POST /admin/tenants/:id/rotate`
- `GET /admin/payments`, `GET /admin/ledger`
- `GET /admin/webhook-deliveries`, `POST /admin/webhook-deliveries/:id/replay`
- `GET /admin/audit-logs` (cursor-paginated: `limit`, `cursor`, `next_cursor`)

Every admin change (tenant create/update/rotate, password change, replay) is
recorded in the audit log with who, what, when and before/after values,
never secrets.

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

## Contributing and security

See [CONTRIBUTING.md](CONTRIBUTING.md). Report vulnerabilities privately as
described in [SECURITY.md](SECURITY.md).

## License

[MIT](LICENSE)
