# KeiPay developer tasks.
#
# Common entrypoints:
#   make setup      create .env, install deps, start postgres + redis, migrate + seed
#   make dev        run backend + frontend together (Ctrl-C stops both)
#   make backend    run only the Go API + worker
#   make frontend   run only the Vite dev server
#   make gen        regenerate frontend API types from api/openapi.yaml
#   make check      fast checks: vet, unit tests, API lint, typecheck, frontend tests
#   make ci         the full pre-merge gate (needs Docker); see scripts/ci.sh
#   make build      build the frontend, then the backend binaries
#   make docker     build the production image
#
# Ports come from .env (HTTP_PORT, UI_PORT, POSTGRES_PORT, REDIS_PORT) and can
# be overridden per command, e.g. `make dev API_PORT=18080 UI_PORT=15173`.
# `make setup` with overrides writes them into a new .env.

BACKEND_DIR := backend
FRONTEND_DIR := frontend
BIN := keipay
GEN_TS := $(FRONTEND_DIR)/src/shared/api/schema.d.ts

# env_get reads KEY from .env (last match wins, quotes stripped); empty if unset.
env_get = $(shell [ -f .env ] && sed -n 's/^$(1)=//p' .env | tail -n 1 | tr -d '"\r')

API_PORT ?= $(or $(call env_get,HTTP_PORT),8080)
UI_PORT ?= $(or $(call env_get,UI_PORT),5173)
POSTGRES_PORT ?= $(or $(call env_get,POSTGRES_PORT),5432)
REDIS_PORT ?= $(or $(call env_get,REDIS_PORT),6379)

.PHONY: help setup env install dev backend frontend up down migrate seed reset-admin \
	build build-frontend build-backend test vet fmt fmt-check typecheck test-frontend \
	gen gen-check lint-api check ci e2e docker clean

## help: list targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## setup: create .env with fresh secrets, install deps, start postgres + redis, migrate and seed.
setup: env install up seed

## env: create .env from .env.example with generated secrets and the chosen ports (never overwrites).
env:
	@if [ -f .env ]; then echo ".env exists, leaving it untouched"; else \
		sed -e "s/^JWT_SECRET=.*/JWT_SECRET=$$(openssl rand -hex 32)/" \
		    -e "s/^ENCRYPTION_KEY=.*/ENCRYPTION_KEY=$$(openssl rand -hex 32)/" \
		    -e "s/^HTTP_PORT=.*/HTTP_PORT=$(API_PORT)/" \
		    -e "s#^HTTP_BASE_URL=.*#HTTP_BASE_URL=http://localhost:$(API_PORT)#" \
		    -e "s/^UI_PORT=.*/UI_PORT=$(UI_PORT)/" \
		    -e "s#@localhost:[0-9]*/#@localhost:$(POSTGRES_PORT)/#" \
		    -e "s/^REDIS_ADDR=.*/REDIS_ADDR=localhost:$(REDIS_PORT)/" \
		    -e "s/^POSTGRES_PORT=.*/POSTGRES_PORT=$(POSTGRES_PORT)/" \
		    -e "s/^REDIS_PORT=.*/REDIS_PORT=$(REDIS_PORT)/" \
		    .env.example > .env; \
		echo "Created .env (API :$(API_PORT), console :$(UI_PORT), postgres :$(POSTGRES_PORT), redis :$(REDIS_PORT))"; \
		echo "Set XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN in .env"; \
	fi

## install: install frontend deps and download Go modules.
install:
	cd $(FRONTEND_DIR) && npm ci
	cd $(BACKEND_DIR) && go mod download

## dev: run backend and frontend together; the UI starts once the API is healthy.
dev:
	@echo "Starting KeiPay: API http://localhost:$(API_PORT), console http://localhost:$(UI_PORT)"
	@trap 'trap - INT TERM EXIT; kill 0' INT TERM EXIT; \
	( cd $(BACKEND_DIR) && HTTP_PORT=$(API_PORT) go run ./cmd/server ) & \
	( \
		ready=0; \
		for i in $$(seq 1 120); do \
			if curl -sf http://127.0.0.1:$(API_PORT)/healthz >/dev/null 2>&1; then ready=1; break; fi; \
			sleep 0.5; \
		done; \
		if [ "$$ready" -ne 1 ]; then echo "Backend did not become healthy on :$(API_PORT)"; exit 1; fi; \
		cd $(FRONTEND_DIR) && VITE_API_TARGET=http://localhost:$(API_PORT) npm run dev -- --port $(UI_PORT) --strictPort \
	) & \
	wait

## backend: run only the Go API + worker on API_PORT.
backend:
	cd $(BACKEND_DIR) && HTTP_PORT=$(API_PORT) go run ./cmd/server

## frontend: run only the Vite dev server on UI_PORT (proxies the API on API_PORT).
frontend:
	cd $(FRONTEND_DIR) && VITE_API_TARGET=http://localhost:$(API_PORT) npm run dev -- --port $(UI_PORT) --strictPort

## up: start postgres + redis for local development and wait until healthy.
up:
	POSTGRES_PORT=$(POSTGRES_PORT) REDIS_PORT=$(REDIS_PORT) docker compose up -d --wait postgres redis

## down: stop every compose service.
down:
	docker compose down

## migrate: apply database migrations.
migrate:
	cd $(BACKEND_DIR) && go run ./cmd/migrate

## seed: apply migrations, create the demo tenant and admin user (credentials printed once).
seed:
	cd $(BACKEND_DIR) && go run ./cmd/migrate -seed

## reset-admin: set the admin password, e.g. make reset-admin PW=yourpass (random if empty).
reset-admin:
	cd $(BACKEND_DIR) && go run ./cmd/migrate -reset-admin -password '$(PW)'

## build: build the frontend assets, then the backend binaries.
build: build-frontend build-backend

build-frontend:
	cd $(FRONTEND_DIR) && npm run build

build-backend:
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.Version=$$(git describe --tags --always --dirty 2>/dev/null || echo dev)" -o ../$(BIN) ./cmd/server
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o ../$(BIN)-migrate ./cmd/migrate

## test: run the backend test suite (Postgres integration tests skip without KEI_TEST_POSTGRES_DSN).
test:
	cd $(BACKEND_DIR) && go test ./...

## vet: run go vet.
vet:
	cd $(BACKEND_DIR) && go vet ./...

## fmt: format Go code.
fmt:
	cd $(BACKEND_DIR) && gofmt -w ./cmd ./internal

## fmt-check: fail if any Go file is not gofmt-formatted.
fmt-check:
	@out=$$(cd $(BACKEND_DIR) && gofmt -l ./cmd ./internal); \
	if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi

## typecheck: type-check the frontend.
typecheck:
	npm --prefix $(FRONTEND_DIR) run typecheck

## test-frontend: run the frontend unit and component tests.
test-frontend:
	npm --prefix $(FRONTEND_DIR) test

## gen: regenerate frontend API types from api/openapi.yaml.
gen:
	npm --prefix $(FRONTEND_DIR) run gen:api

## gen-check: regenerate API types and fail if they changed (commit the result).
gen-check:
	@tmp=$$(mktemp); cp $(GEN_TS) $$tmp 2>/dev/null || : ; \
	npm --prefix $(FRONTEND_DIR) run --silent gen:api >/dev/null || { rm -f $$tmp; exit 1; }; \
	if cmp -s $$tmp $(GEN_TS); then rm -f $$tmp; \
	else rm -f $$tmp; echo "$(GEN_TS) was out of date with api/openapi.yaml; regenerated it, review and commit"; exit 1; fi

## lint-api: lint api/openapi.yaml.
lint-api:
	npm --prefix $(FRONTEND_DIR) run lint:api

## check: fast checks without Docker: gofmt, vet, unit tests, API lint, typecheck, frontend tests.
check: fmt-check vet test lint-api typecheck test-frontend

## ci: the full pre-merge gate, with Postgres + Redis in a throwaway compose project.
ci:
	./scripts/ci.sh

## e2e: run the Playwright smoke test against a throwaway stack.
e2e:
	./scripts/ci.sh e2e

## docker: build the production image (API + worker + console).
docker:
	docker build -f deploy/Dockerfile -t keipay:local .

## clean: remove build output.
clean:
	rm -f $(BIN) $(BIN)-migrate
	rm -rf $(FRONTEND_DIR)/dist
