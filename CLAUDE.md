# CLAUDE.md

Guidance for Claude Code (claude.ai/code) when working in this repository.

## What this is

KeiPay is a self-hosted payment orchestrator. Apps (formerly tenants) call one
API with their own API keys; KeiPay creates the checkout on the payment gateway
through the app's default connection (today: a Xendit Invoice on the
operator's own Xendit account), receives the gateway webhook on that
connection's own URL, and forwards a normalized, signed event to the
originating app. KeiPay never holds funds. See `README.md` for the API and
webhook contracts, and `docs/design.md` for the multi-gateway design (routing,
attempts, Midtrans/DOKU adapters) that is not built yet.

## Repository layout

Monorepo, deployed as one image:

- `backend/`: Go module `github.com/mydisha/keipay/backend` (API, asynq worker,
  migrations). Commands run from `backend/`; config reads `backend/.env`, then
  the root `.env`.
- `api/openapi.yaml`: the HTTP contract (OpenAPI 3.1, linted with
  `api/redocly.yaml`). Frontend types in
  `frontend/src/shared/api/schema.d.ts` are generated from it (`make gen`).
- `frontend/`: React 19 + Vite + Tailwind v4 + shadcn/ui admin console, UI
  rules in `DESIGN.md`. In dev, Vite on `UI_PORT` proxies the API to
  `VITE_API_TARGET` (default http://localhost:8080). In production the backend serves the
  build (`FRONTEND_DIR`, or `../frontend/dist` auto-detected) via
  `handler.SPA`; `/v1`, `/admin`, `/healthz` and everything under `/webhooks/`
  keep JSON 404s. Exactly `/webhooks` is the console's Webhooks page.
- `deploy/Dockerfile`: one image (API + worker + console + migrate binary).
- `compose.yaml`: postgres, redis, one-shot migrate, keipay; `asynqmon` under
  the `tools` profile.

## Commands

```bash
make setup       # .env with generated secrets, deps, postgres + redis, migrate + seed
make dev         # API :8080 + console :5173 together
make backend     # API + worker only
make frontend    # Vite only
make up / down   # postgres + redis
make seed        # migrations + demo test app (prints its key and secrets once)
make reset-admin EMAIL=you@example.com PW=yourpass   # recovery; first owner comes from console setup
make test        # go test ./... in backend/
make gen         # regenerate frontend API types from api/openapi.yaml
make check       # gofmt + vet + test + API lint + typecheck + frontend tests
make ci          # full pre-merge gate (scripts/ci.sh): adds Postgres/Redis
                 # integration tests, gen drift check, build, Playwright smoke
make e2e         # Playwright smoke only
make build       # frontend dist, then ./keipay and ./keipay-migrate
make docker      # keipay:local image

cd backend && go test ./internal/service/... -run TestWebhook   # one test
```

## Backend architecture

Service-layer (ports-and-adapters); dependencies point inward to
`internal/domain`, which imports only the standard library.

- `internal/domain`: entities, enums, errors, repository and service interfaces.
- `internal/service`: business rules and validation; returns domain errors
  (`domain.ErrNotFound`, `ErrInvalidInput`, `ErrConflict`, ...); never imports
  transport types.
- `internal/transport/http`: thin Gin handlers (parse → call service →
  respond), middleware, DTOs, `response.Error` for error mapping. `router.go` is
  the only place routes are registered.
- `internal/repository/{postgres,redis}`: implementations of domain interfaces,
  no business logic.
- `internal/worker`: asynq enqueuer, webhook forwarder, sweeper.
- `internal/provider`: gateway port (`Provider`, `Registry`, schemas,
  capabilities, error classes); one adapter package per gateway
  (`internal/provider/xendit`), registered in `cmd/server/main.go`.
- `internal/pkg`: crypto (AES-256-GCM), hmac, keygen, logger.
- Wiring is manual DI in `cmd/server/main.go`.

New endpoint: spec in `api/openapi.yaml` → input types in domain → logic in
service → DTO → handler → route in `router.go` → `make gen`. New entity: domain struct + repository interface →
postgres implementation → migration in `backend/migrations/` → wire in main.

## Things that are not obvious from one file

1. **App identification is always a table lookup**: `/v1` hashes
   `X-Api-Key` (SHA-256) and loads key + app in one query by `key_hash`; the
   key decides the app and its environment (`kp_test_`/`kp_live_`, legacy
   `kp_` keys take the app's). Gateway callbacks arrive at
   `/webhooks/{provider}/{con_…}`, are verified with that connection's
   credentials, and find the payment by its stored reference
   (`payments.external_id`: `kp_pay_…`, or `{slug}:{order_id}` for pre-apps
   payments), which must belong to the same connection; nothing parses a
   slug. `/webhooks/xendit` aliases the connection imported from the
   deprecated `XENDIT_*` env (`merchants.legacy_connection_id`). The legacy
   `tenants` table and `tenant_id` columns are read-only leftovers of
   expand/contract.
2. **Two-layer idempotency**: `WebhookService.Process` checks Redis
   (`idemp.Seen` on `{xenditID}:{status}`), then runs the durable guard
   (`CreateEventIfNew`) in the same transaction as the status update, ledger
   entry and delivery row (`domain.TxManager`). The Redis key is set only after
   commit. The payment row is locked (`GetByExternalIDForUpdate`); status only
   moves forward per `PaymentStatus.CanTransitionTo`; a callback whose amount
   or currency differs is recorded, not applied (`service.ErrCallbackMismatch`).
   `PaymentService.Create` is idempotent per `(app_id, idempotency_key)`
   (checked first, with a request fingerprint) and per `(app_id, order_id)`.
3. **HMAC both ways, different secrets**: inbound `/v1/*` signs
   `"{timestamp}.{rawBody}"` with the secret of the API key used (+ Redis
   nonce); outbound forwards sign the raw body as `X-Kei-Signature` with the
   app's webhook signing secret (for migrated apps, their old `api_secret`),
   so key rotation never breaks webhook verification. Secrets are AES-256-GCM
   encrypted at rest and shown in plaintext only at create/rotate. Gateway
   credentials use envelope encryption (per-connection data key, bound to the
   connection's public id) and are never returned, only their last 4 chars.
4. **Delivery durability**: `Process` writes a `pending` `webhook_deliveries`
   row in the event transaction and enqueues after commit (task ID derived from
   the delivery id). If enqueue fails, `worker.Sweeper` re-enqueues pending
   deliveries with `attempts = 0` older than a minute. Admins can replay.
5. **Admin sessions**: the session id in the `keipay_session` cookie is only
   a Redis key (stored hashed); every authenticated request still loads the
   user from Postgres, so disabling a user or changing a role takes effect at
   once. Each session records the user's `session_version`; bumping it
   (disable, password change or reset) ends all other sessions without
   tracking them. Unsafe `/admin` methods need `X-CSRF-Token` equal to the
   `keipay_csrf` cookie and the session's token. Permissions per role are data
   in `internal/domain`; each route declares one in `router.go`, and a test
   checks them against `x-keipay-permission` in the spec.

## Conventions

- Errors: `{"error":{"code","message","details"}}`; codes are constants in
  `internal/domain`, mapped only in `response.Error`, and mirrored in the
  spec's `ErrorCode`.
- Public IDs: `<prefix>_<ULID>` via `internal/pkg/id`; bigint ids stay
  internal (`/v1` `payment_id`, delivery and ledger ids are the remaining
  exceptions until their entity migrates).
- New lists use cursor pagination (`limit`, `cursor` → `data`,
  `next_cursor`); see `internal/domain/pagination.go`.
- Every admin mutation records an audit entry in the same transaction via
  `AuditService`; the actor comes from the request context.

- `gofmt` and `go vet` before committing; lowercase, concise error messages;
  return errors rather than panic; context for request scope.
- Commits: small, Conventional Commits (`feat(backend): ...`).
- New logic and bug fixes need tests (happy, edge, error path; regression test
  for fixes).
