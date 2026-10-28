# Security Policy

## Reporting a vulnerability

Please report security issues privately. **Do not open a public GitHub issue.**

1. **GitHub private vulnerability reporting** (preferred):
   [open an advisory](https://github.com/mydisha/keipay/security/advisories/new)
2. **Email**: contact the maintainers through the details on the repository profile

Include a description, steps to reproduce, the impact, and a suggested fix if
you have one.

We aim to acknowledge reports within 72 hours and to share a fix or mitigation
plan within 7 days for confirmed issues.

## Supported versions

KeiPay has not reached a stable release. Only the latest `main` receives fixes.

## Scope

KeiPay sits between your apps and your payment gateway and stores gateway and
tenant secrets, so these areas matter most:

- Tenant request signing and replay protection (`backend/internal/transport/http/middleware/tenant_auth.go`)
- Gateway webhook verification (`backend/internal/transport/http/middleware/callback_token.go`)
- Webhook processing, idempotency and amount checks (`backend/internal/service/webhook_service.go`)
- Secret encryption at rest (`backend/internal/pkg/crypto/`)
- Outbound webhook signing (`backend/internal/worker/forwarder.go`)
- Admin authentication (`backend/internal/service/auth_service.go`, `middleware/admin_auth.go`)

## Operator responsibilities

- Keep `ENCRYPTION_KEY` and `JWT_SECRET` out of version control and back up
  `ENCRYPTION_KEY`: losing it makes stored tenant secrets unrecoverable.
- Use gateway API keys with the narrowest permissions the gateway offers.
- Serve KeiPay over HTTPS; gateway webhooks and tenant requests carry secrets
  and payment data.
