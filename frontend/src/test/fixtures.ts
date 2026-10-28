import type { components } from "@/shared/api/schema";

type Schemas = components["schemas"];

export const APP_ID = "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C";
export const PAY_ID = "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0D";

export function app(overrides: Partial<Schemas["App"]> = {}): Schemas["App"] {
  return {
    id: APP_ID,
    name: "Demo App",
    slug: "demo",
    environment: "test",
    status: "active",
    metadata: {},
    webhook_url: "https://demo.example.com/webhooks/kei",
    redirect_success_url: "",
    redirect_failure_url: "",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

/** The `app` summary other rows carry; follows `app()` unless overridden. */
export function appSummary(overrides: Partial<Schemas["AppSummary"]> = {}): Schemas["AppSummary"] {
  const { id, slug, name, environment } = app();
  return { id, slug, name, environment, ...overrides };
}

export function apiKey(overrides: Partial<Schemas["ApiKey"]> = {}): Schemas["ApiKey"] {
  return {
    id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0E",
    app_id: APP_ID,
    label: "Default",
    environment: "test",
    prefix: "kp_test_",
    last4: "a1b2",
    status: "active",
    last_used_at: null,
    expires_at: null,
    revoked_at: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function appCreated(overrides: Partial<Schemas["App"]> = {}): Schemas["AppCreatedResponse"] {
  return {
    app: app(overrides),
    key: apiKey(),
    api_key: "kp_test_0123456789abcdef0123456789abcdef",
    api_secret: "kps_test_secret-value-shown-once",
    webhook_secret: "kpwh_webhook-secret-shown-once",
  };
}

export function payment(overrides: Partial<Schemas["Payment"]> = {}): Schemas["Payment"] {
  return {
    id: PAY_ID,
    reference: `kp_${PAY_ID}`,
    app: appSummary(),
    order_id: "order-001",
    xendit_invoice_id: "inv_1",
    amount: 150000,
    currency: "IDR",
    status: "PAID",
    description: "",
    invoice_url: "https://checkout.example.com/inv_1",
    payer_email: "",
    items: [],
    metadata: {},
    expires_at: null,
    paid_at: null,
    settled_at: null,
    expected_settlement_at: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function delivery(overrides: Partial<Schemas["WebhookDelivery"]> = {}): Schemas["WebhookDelivery"] {
  return {
    id: 7,
    payment_id: PAY_ID,
    app: appSummary(),
    event_type: "payment.paid",
    payload: {
      event: "payment.paid",
      payment_id: 10,
      payment_public_id: PAY_ID,
      order_id: "order-001",
      external_id: `kp_${PAY_ID}`,
      status: "PAID",
      amount: 150000,
      currency: "IDR",
      metadata: {},
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

/** A cursor list body. */
export function page<T>(data: T[], nextCursor: string | null = null) {
  return { data, next_cursor: nextCursor };
}

export function auditLog(overrides: Partial<Schemas["AuditLog"]> = {}): Schemas["AuditLog"] {
  return {
    id: "aud_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    occurred_at: "2026-01-01T00:00:00Z",
    actor: { type: "system", id: "seed" },
    action: "app.create",
    target: { type: "app", id: APP_ID },
    before: null,
    after: { id: APP_ID, name: "Demo App", slug: "demo", environment: "test", status: "active" },
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
    "apps:read",
    "apps:write",
    "audit:read",
    "deliveries:read",
    "deliveries:replay",
    "payments:read",
    "users:manage",
  ],
  admin: ["apps:read", "apps:write", "audit:read", "deliveries:read", "deliveries:replay", "payments:read"],
  viewer: ["apps:read", "deliveries:read", "payments:read"],
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
