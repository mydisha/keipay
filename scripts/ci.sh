#!/usr/bin/env bash
# KeiPay pre-merge gate. Runs everything CI would run, locally.
#
#   scripts/ci.sh            all stages: static, backend, frontend, e2e
#   scripts/ci.sh e2e        only the named stages (any of the four)
#
# Postgres and Redis run in a throwaway compose project (default keipay-ci)
# on non-default host ports, so the gate can run next to `make dev`. The
# project and its volumes are removed on exit, including on failure or Ctrl-C.
#
# Overrides: CI_COMPOSE_PROJECT, CI_POSTGRES_PORT (25432), CI_REDIS_PORT
# (26379), CI_API_PORT (28080), CI_NPM_INSTALL=0 to skip `npm ci`.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

PROJECT="${CI_COMPOSE_PROJECT:-keipay-ci}"
PG_PORT="${CI_POSTGRES_PORT:-25432}"
REDIS_PORT="${CI_REDIS_PORT:-26379}"
API_PORT="${CI_API_PORT:-28080}"
GEN_TS="frontend/src/shared/api/schema.d.ts"

STAGES=("$@")
if [[ ${#STAGES[@]} -eq 0 ]]; then
  STAGES=(static backend frontend e2e)
fi

WORK="$(mktemp -d)"
STACK_UP=0
SERVER_PID=""
FRONTEND_BUILT=0
DEPS_INSTALLED=0

log() { printf '\n==> %s\n' "$*"; }
fail() { printf '\nci: %s\n' "$*" >&2; exit 1; }

cleanup() {
  local code=$?
  trap - EXIT INT TERM
  if [[ -n "$SERVER_PID" ]]; then
    kill "$SERVER_PID" 2>/dev/null || true
    wait "$SERVER_PID" 2>/dev/null || true
  fi
  if [[ "$STACK_UP" -eq 1 ]]; then
    docker compose -p "$PROJECT" down -v --remove-orphans >/dev/null 2>&1 || true
  fi
  rm -rf "$WORK"
  if [[ $code -eq 0 ]]; then
    printf '\nci: all stages passed (%s)\n' "${STAGES[*]}"
  else
    printf '\nci: FAILED (exit %d)\n' "$code" >&2
  fi
  exit "$code"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

# Configuration for every Go process started here. Process env wins over any
# .env file, so a developer's .env cannot leak into the gate.
export APP_ENV=development
export LOG_LEVEL=warn
export POSTGRES_DSN="postgres://keipay:keipay@localhost:${PG_PORT}/keipay?sslmode=disable"
export REDIS_ADDR="localhost:${REDIS_PORT}"
export REDIS_PASSWORD=""
export REDIS_DB=0
ENCRYPTION_KEY="$(openssl rand -hex 32)"
export ENCRYPTION_KEY
# Deprecated settings, kept on purpose: the e2e server imports them once into
# the `xendit-imported` test connection (the seeded demo app's default), which
# exercises the upgrade path. The key is fake, so no checkout ever succeeds.
export XENDIT_SECRET_KEY="xnd_development_ci"
export XENDIT_CALLBACK_TOKEN="ci-callback-token"
export KEI_TEST_POSTGRES_DSN="$POSTGRES_DSN"
export KEI_TEST_REDIS_ADDR="$REDIS_ADDR"

frontend_deps() {
  [[ "$DEPS_INSTALLED" -eq 1 ]] && return
  if [[ "${CI_NPM_INSTALL:-1}" == "0" && -d frontend/node_modules ]]; then
    log "frontend deps: skipped (CI_NPM_INSTALL=0)"
  else
    log "frontend deps: npm ci"
    npm --prefix frontend ci --no-audit --no-fund
  fi
  DEPS_INSTALLED=1
}

stack_up() {
  [[ "$STACK_UP" -eq 1 ]] && return
  log "stack: postgres :${PG_PORT}, redis :${REDIS_PORT} (project ${PROJECT})"
  # A run killed with SIGKILL leaves its project behind; start clean.
  docker compose -p "$PROJECT" down -v --remove-orphans >/dev/null 2>&1 || true
  STACK_UP=1
  POSTGRES_PASSWORD=keipay POSTGRES_PORT="$PG_PORT" REDIS_PORT="$REDIS_PORT" \
    docker compose -p "$PROJECT" up -d --wait postgres redis
  log "migrations: up, down, up"
  (cd backend && go run ./cmd/migrate)
  (cd backend && go run ./cmd/migrate -down)
  (cd backend && go run ./cmd/migrate)
}

build_frontend() {
  [[ "$FRONTEND_BUILT" -eq 1 ]] && return
  frontend_deps
  log "frontend: build"
  npm --prefix frontend run build
  FRONTEND_BUILT=1
}

stage_static() {
  log "go: gofmt"
  local unformatted
  unformatted="$(cd backend && gofmt -l ./cmd ./internal)"
  [[ -z "$unformatted" ]] || fail "gofmt needed:"$'\n'"$unformatted"
  log "go: vet"
  (cd backend && go vet ./...)
  frontend_deps
  log "api: lint"
  npm --prefix frontend run lint:api
  log "api: generated types in sync"
  local before="$WORK/schema.before"
  cp "$GEN_TS" "$before" 2>/dev/null || : >"$before"
  npm --prefix frontend run --silent gen:api >/dev/null
  cmp -s "$before" "$GEN_TS" || fail "$GEN_TS was stale; it has been regenerated, review and commit it"
}

stage_backend() {
  stack_up
  log "go: test (with Postgres and Redis integration tests)"
  (cd backend && go test -count=1 ./...)
}

stage_frontend() {
  frontend_deps
  log "frontend: typecheck"
  npm --prefix frontend run typecheck
  log "frontend: test"
  npm --prefix frontend test
  build_frontend
}

stage_e2e() {
  stack_up
  build_frontend
  if curl -sf "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then
    fail "port ${API_PORT} is already serving; set CI_API_PORT"
  fi

  log "e2e: build server, seed"
  (cd backend && CGO_ENABLED=0 go build -o "$WORK/keipay" ./cmd/server)
  (cd backend && CGO_ENABLED=0 go build -o "$WORK/keipay-migrate" ./cmd/migrate)
  local password
  password="$(openssl rand -hex 12)"
  # Seed output contains one-time secrets; keep it out of the log.
  (cd backend && "$WORK/keipay-migrate" -seed >/dev/null)

  log "e2e: start server on :${API_PORT}"
  (cd backend && exec env HTTP_PORT="$API_PORT" HTTP_BASE_URL="http://127.0.0.1:${API_PORT}" \
    FRONTEND_DIR="$ROOT/frontend/dist" RUN_WORKER=true "$WORK/keipay") >"$WORK/server.log" 2>&1 &
  SERVER_PID=$!
  local ready=0
  for _ in $(seq 1 120); do
    if curl -sf "http://127.0.0.1:${API_PORT}/healthz" >/dev/null 2>&1; then ready=1; break; fi
    kill -0 "$SERVER_PID" 2>/dev/null || break
    sleep 0.5
  done
  if [[ "$ready" -ne 1 ]]; then
    tail -n 50 "$WORK/server.log" >&2 || true
    fail "server did not become healthy on :${API_PORT}"
  fi

  # No user exists yet: the Playwright `setup` project creates the owner
  # through the console's first-run setup with E2E_EMAIL and E2E_PASSWORD,
  # and every other project signs in as that owner.
  log "e2e: playwright"
  npm --prefix frontend exec -- playwright install chromium
  if ! E2E_BASE_URL="http://127.0.0.1:${API_PORT}" E2E_EMAIL="owner@example.com" E2E_PASSWORD="$password" \
    npm --prefix frontend run e2e; then
    log "server log (last 50 lines)"
    tail -n 50 "$WORK/server.log" >&2 || true
    fail "e2e failed"
  fi
}

for stage in "${STAGES[@]}"; do
  case "$stage" in
    static) stage_static ;;
    backend) stage_backend ;;
    frontend) stage_frontend ;;
    e2e) stage_e2e ;;
    *) fail "unknown stage: $stage (use static, backend, frontend, e2e)" ;;
  esac
done
