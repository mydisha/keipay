# syntax=docker/dockerfile:1

# One image runs the API, the webhook worker and the admin console.

FROM node:22-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ .
RUN npm run build

FROM golang:1.26-alpine AS backend
WORKDIR /src/backend
# VERSION falls back to SOURCE_COMMIT (injected by some PaaS builders), then "dev".
ARG VERSION=dev
ARG SOURCE_COMMIT=
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/ .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    VER="${VERSION}"; \
    if [ -z "${VER}" ] || [ "${VER}" = "dev" ]; then \
      if [ -n "${SOURCE_COMMIT}" ]; then VER="${SOURCE_COMMIT}"; else VER="dev"; fi; \
    fi; \
    echo "Building keipay version: ${VER}"; \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.Version=${VER}" \
      -o /out/keipay ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" \
      -o /out/keipay-migrate ./cmd/migrate

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata curl \
    && addgroup -g 1001 keipay \
    && adduser -D -u 1001 -G keipay keipay

# migrate reads file://migrations relative to the working directory.
WORKDIR /app
COPY --from=backend /out/keipay /app/keipay
COPY --from=backend /out/keipay-migrate /app/keipay-migrate
COPY backend/migrations /app/migrations
COPY --from=frontend /src/frontend/dist /app/frontend

ENV HTTP_PORT=8080 \
    FRONTEND_DIR=/app/frontend

USER keipay
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD curl -fsS http://localhost:8080/healthz || exit 1

ENTRYPOINT ["/app/keipay"]
