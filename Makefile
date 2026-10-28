# KeiPay developer tasks.
#
# Common entrypoints:
#   make setup      create .env, install deps, start postgres + redis, migrate + seed
#   make dev        run backend + frontend together (Ctrl-C stops both)
#   make backend    run only the Go API + worker
#   make frontend   run only the Vite dev server
#   make build      build the frontend, then the backend binaries
#   make test       run backend tests
#   make docker     build the production image

BACKEND_DIR := backend
FRONTEND_DIR := frontend
BIN := keipay
API_PORT := 8080
UI_PORT := 5173

.PHONY: help setup env install dev backend frontend up down migrate seed reset-admin \
	build build-frontend build-backend test vet fmt typecheck check docker clean

## help: list targets.
help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/^## /  /'

## setup: create .env with fresh secrets, install deps, start postgres + redis, migrate and seed.
setup: env install up seed

## env: create .env from .env.example with generated JWT_SECRET and ENCRYPTION_KEY (never overwrites).
env:
	@if [ -f .env ]; then echo ".env exists, leaving it untouched"; else \
		sed -e "s/^JWT_SECRET=.*/JWT_SECRET=$$(openssl rand -hex 32)/" \
		    -e "s/^ENCRYPTION_KEY=.*/ENCRYPTION_KEY=$$(openssl rand -hex 32)/" .env.example > .env; \
		echo "Created .env with generated secrets; set XENDIT_SECRET_KEY and XENDIT_CALLBACK_TOKEN"; \
	fi

## install: install frontend deps and download Go modules.
install:
	cd $(FRONTEND_DIR) && npm ci
	cd $(BACKEND_DIR) && go mod download

## dev: run backend and frontend together; the UI starts once the API is healthy.
dev:
	@echo "Starting KeiPay: API http://localhost:$(API_PORT), console http://localhost:$(UI_PORT)"
	@trap 'trap - INT TERM EXIT; kill 0' INT TERM EXIT; \
	( cd $(BACKEND_DIR) && go run ./cmd/server ) & \
	( \
		ready=0; \
		for i in $$(seq 1 120); do \
			if curl -sf http://127.0.0.1:$(API_PORT)/healthz >/dev/null 2>&1; then ready=1; break; fi; \
			sleep 0.5; \
		done; \
		if [ "$$ready" -ne 1 ]; then echo "Backend did not become healthy on :$(API_PORT)"; exit 1; fi; \
		cd $(FRONTEND_DIR) && npm run dev -- --port $(UI_PORT) --strictPort \
	) & \
	wait

## backend: run only the Go API + worker.
backend:
	cd $(BACKEND_DIR) && go run ./cmd/server

## frontend: run only the Vite dev server (proxies /admin and /v1 to the API).
frontend:
	cd $(FRONTEND_DIR) && npm run dev -- --port $(UI_PORT) --strictPort

## up: start postgres + redis for local development and wait until healthy.
up:
	docker compose up -d --wait postgres redis

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

## test: run the backend test suite.
test:
	cd $(BACKEND_DIR) && go test ./...

## vet: run go vet.
vet:
	cd $(BACKEND_DIR) && go vet ./...

## fmt: format Go code.
fmt:
	cd $(BACKEND_DIR) && gofmt -w ./cmd ./internal

## typecheck: type-check the frontend.
typecheck:
	cd $(FRONTEND_DIR) && npx tsc --noEmit -p .

## check: vet, test and typecheck.
check: vet test typecheck

## docker: build the production image (API + worker + console).
docker:
	docker build -f deploy/Dockerfile -t keipay:local .

## clean: remove build output.
clean:
	rm -f $(BIN) $(BIN)-migrate
	rm -rf $(FRONTEND_DIR)/dist
