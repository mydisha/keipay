import type { components } from "@/shared/api/schema";

type Schemas = components["schemas"];

export function tenant(overrides: Partial<Schemas["Tenant"]> = {}): Schemas["Tenant"] {
  return {
    id: 1,
    name: "Demo App",
    slug: "demo",
    api_key: "kei_demo_key_0001",
    webhook_url: "https://demo.example.com/webhooks/kei",
    redirect_success_url: "",
    redirect_failure_url: "",
    active: true,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function payment(overrides: Partial<Schemas["Payment"]> = {}): Schemas["Payment"] {
  return {
    id: 10,
    tenant_id: 1,
    order_id: "order-001",
    external_id: "demo:order-001",
    xendit_invoice_id: "inv_1",
    amount: 150000,
    currency: "IDR",
    status: "PAID",
    description: "",
    invoice_url: "https://checkout.example.com/inv_1",
    payer_email: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function delivery(overrides: Partial<Schemas["WebhookDelivery"]> = {}): Schemas["WebhookDelivery"] {
  return {
    id: 7,
    payment_id: 10,
    tenant_id: 1,
    event_type: "payment.paid",
    payload: {
      event: "payment.paid",
      payment_id: 10,
      order_id: "order-001",
      external_id: "demo:order-001",
      status: "PAID",
      amount: 150000,
      currency: "IDR",
      timestamp: "2026-01-01T00:00:00Z",
    },
    status: "FAILED",
    attempts: 3,
    last_error: "connection refused",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function auditLog(overrides: Partial<Schemas["AuditLog"]> = {}): Schemas["AuditLog"] {
  return {
    id: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    occurred_at: "2026-01-01T00:00:00Z",
    actor: { type: "system", id: "seed" },
    action: "tenant.create",
    target: { type: "tenant", id: "1" },
    before: null,
    after: { id: 1, name: "Demo App", slug: "demo", api_key_last4: "0001", active: true },
    ip: "",
    user_agent: "",
    request_id: "",
    ...overrides,
  };
}

// Mirrors the permission table in api/openapi.yaml (test data only; the
// console itself never maps roles to permissions).
export const ROLE_PERMISSIONS: Record<Schemas["UserRole"], Schemas["Permission"][]> = {
  owner: [
    "audit:read",
    "deliveries:read",
    "deliveries:replay",
    "payments:read",
    "tenants:read",
    "tenants:write",
    "users:manage",
  ],
  admin: ["audit:read", "deliveries:read", "deliveries:replay", "payments:read", "tenants:read", "tenants:write"],
  viewer: ["deliveries:read", "payments:read", "tenants:read"],
};

export function user(overrides: Partial<Schemas["User"]> = {}): Schemas["User"] {
  return {
    id: "usr_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    email: "owner@example.com",
    name: "Olivia Owner",
    role: "owner",
    status: "active",
    must_change_password: false,
    disabled_at: null,
    last_login_at: "2026-01-01T00:00:00Z",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

/** A `GET /admin/me` body for a user; permissions follow the role unless given. */
export function me(
  overrides: Partial<Schemas["User"]> = {},
  permissions?: Schemas["Permission"][],
): Schemas["MeResponse"] {
  const u = user(overrides);
  return { user: u, permissions: permissions ?? ROLE_PERMISSIONS[u.role] };
}
