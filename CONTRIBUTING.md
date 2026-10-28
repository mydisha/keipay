# Contributing to KeiPay

Thanks for helping out. This covers what you need to get started.

## Prerequisites

- Go 1.26+
- Node.js 22+ and npm
- Docker (for Postgres and Redis)
- `openssl`

## Development setup

```bash
git clone https://github.com/mydisha/keipay.git
cd keipay
make setup   # .env with generated secrets, deps, postgres + redis, migrate + seed
make dev     # API on :8080, console on :5173
```

| Command | What it does |
|---|---|
| `make dev` | Run backend and frontend together |
| `make backend` / `make frontend` | Run one side only |
| `make up` / `make down` | Start or stop Postgres and Redis |
| `make test` | Backend tests |
| `make gen` | Regenerate frontend API types from `api/openapi.yaml` |
| `make check` | Fast checks: gofmt, `go vet`, unit tests, API lint, type-check, frontend tests |
| `make ci` | The full pre-merge gate (needs Docker) |
| `make build` | Frontend assets, then backend binaries |
| `make docker` | Production image |

## Project structure

- `backend/` is a Go module using a service-layer (ports-and-adapters) layout:
  thin HTTP handlers in `internal/transport/http`, business rules in
  `internal/service`, entities and interfaces in `internal/domain` (standard
  library only), Postgres and Redis adapters in `internal/repository`.
- `frontend/` is the React admin console. In production the backend serves its
  build, so both ship as one image. UI rules live in `DESIGN.md`.
- `api/openapi.yaml` is the HTTP contract. Change it in the same commit as the
  endpoint, then run `make gen` and commit the regenerated
  `frontend/src/shared/api/schema.d.ts`. Never edit generated types by hand.
- Design notes live in `docs/`. Larger changes should start with a short
  design note there or in an issue.

## Making changes

1. Fork and branch from `main` (`feat/...`, `fix/...`, `docs/...`).
2. Keep commits small and focused, with
   [Conventional Commits](https://www.conventionalcommits.org/) messages such
   as `feat(backend): ...` or `fix(frontend): ...`.
3. New logic and bug fixes come with tests: the happy path, edge cases and the
   error path. A bug fix needs a regression test that fails without the fix.
4. Run `make check` while you work. **`make ci` is the gate before merge**:
   it must pass locally before a pull request is merged. A hosted CI, when
   added, runs the same target.
5. Open a pull request against `main` describing what changed and why.

## Code style

- Go: `gofmt` (`make fmt`) and `go vet`. Return errors instead of panicking;
  services return domain errors (`domain.ErrNotFound`, `domain.ErrInvalidInput`,
  ...) and handlers map them with `response.Error`.
- TypeScript: strict mode; `make typecheck` must pass.
- API errors use the envelope `{"error":{"code","message","details"}}` with a
  stable `code` from `internal/domain`; add new codes to the spec's
  `ErrorCode` too.

## Reporting security issues

Do not open public issues for vulnerabilities. See [SECURITY.md](SECURITY.md).
