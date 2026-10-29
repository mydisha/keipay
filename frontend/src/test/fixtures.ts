import type { components } from "@/shared/api/schema";

type Schemas = components["schemas"];

export const APP_ID = "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0C";
export const PAY_ID = "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0D";
export const CON_ID = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0G";

export function app(overrides: Partial<Schemas["App"]> = {}): Schemas["App"] {
  return {
    id: APP_ID,
    name: "Demo App",
    slug: "demo",
    environment: "test",
    status: "active",
    metadata: {},
    redirect_success_url: "",
    redirect_failure_url: "",
    default_connection: null,
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

export const DLV_ID = "dlv_01JBKQ7Z8M4X2Y6V3N9T5R1W0H";
export const EVT_ID = "evt_01JBKQ7Z8M4X2Y6V3N9T5R1W0J";
export const WHE_ID = "whe_01JBKQ7Z8M4X2Y6V3N9T5R1W0K";
export const INB_ID = "inb_01JBKQ7Z8M4X2Y6V3N9T5R1W0M";

export function delivery(overrides: Partial<Schemas["WebhookDelivery"]> = {}): Schemas["WebhookDelivery"] {
  return {
    id: DLV_ID,
    app: appSummary(),
    endpoint: { id: WHE_ID, url: "https://shop.example.com/hooks/keipay", description: "", deleted: false },
    event: { id: EVT_ID, type: "payment.paid", test: false },
    event_type: "payment.paid",
    payment_id: PAY_ID,
    status: "FAILED",
    attempts: 3,
    max_attempts: 3,
    next_attempt_at: null,
    last_attempt_at: "2026-01-01T00:05:00Z",
    last_response_status: 500,
    last_error: "status=500",
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:05:00Z",
    ...overrides,
  };
}

export function deliveryAttempt(
  overrides: Partial<Schemas["WebhookDeliveryAttempt"]> = {},
): Schemas["WebhookDeliveryAttempt"] {
  return {
    attempt: 1,
    manual: false,
    url: "https://shop.example.com/hooks/keipay",
    request_headers: {
      "Content-Type": "application/json",
      "X-KeiPay-Event-Id": EVT_ID,
      Authorization: "[REDACTED]",
    },
    response_status: 500,
    response_body: "upstream failed",
    response_body_truncated: false,
    error: "status=500",
    duration_ms: 120,
    started_at: "2026-01-01T00:00:01Z",
    ...overrides,
  };
}

export function deliveryDetail(
  overrides: Partial<Schemas["WebhookDeliveryDetail"]> = {},
): Schemas["WebhookDeliveryDetail"] {
  return {
    ...delivery(),
    payload: {
      id: EVT_ID,
      type: "payment.paid",
      api_version: "v2",
      created_at: "2026-01-01T00:00:00Z",
      data: { id: PAY_ID, order_id: "order-001", amount: 150000, currency: "IDR" },
    },
    attempt_log: [deliveryAttempt()],
    ...overrides,
  };
}

export function webhookEndpoint(overrides: Partial<Schemas["WebhookEndpoint"]> = {}): Schemas["WebhookEndpoint"] {
  return {
    id: WHE_ID,
    app: appSummary(),
    url: "https://shop.example.com/hooks/keipay",
    description: "Order service",
    events: ["payment.paid", "payment.settled"],
    api_version: "v2",
    legacy_signature: false,
    headers: [],
    retry_policy: { max_attempts: 8, backoff: "exponential", interval_seconds: 30 },
    status: "active",
    disabled_reason: null,
    disabled_at: null,
    consecutive_failures: 0,
    secret_last4: "c0de",
    previous_secret_expires_at: null,
    migrated: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export const WEBHOOK_SECRET = "kpwh_0123456789abcdef0123456789abcdef0123456789abcdef";

function exampleEnvelope(type: Schemas["EventType"]): Schemas["WebhookEnvelope"] {
  return {
    id: "evt_01JBKQ7Z8M4X2Y6V3N9T5R1W0C",
    type,
    api_version: "v2",
    created_at: "2026-01-01T10:00:00Z",
    data: {
      id: PAY_ID,
      order_id: "order_1001",
      reference: `kp_${PAY_ID}`,
      status: "PAID",
      amount: 150000,
      currency: "IDR",
      metadata: {},
      provider: "xendit",
      connection_id: CON_ID,
      paid_at: null,
      settled_at: null,
    },
  };
}

/** A catalog shaped like the server's, with fewer types so tests stay short. */
export function eventCatalog(): Schemas["EventTypeListResponse"] {
  const types: [Schemas["EventType"], string][] = [
    ["payment.paid", "The payer paid."],
    ["payment.settled", "The gateway settled the funds."],
    ["payment.expired", "The checkout expired."],
  ];
  return {
    data: types.map(([type, description]) => ({ type, description, example: exampleEnvelope(type) })),
    wildcards: [
      { pattern: "payment.*", description: "Every payment event." },
      { pattern: "*", description: "Every event, including types added later." },
    ],
  };
}

export function inboundWebhook(overrides: Partial<Schemas["InboundWebhook"]> = {}): Schemas["InboundWebhook"] {
  return {
    id: INB_ID,
    connection: connectionSummary(),
    provider: "xendit",
    source: "webhook",
    signature_valid: true,
    result: "applied",
    result_detail: "",
    event_key: "inv_123",
    provider_status: "PAID",
    reference: `kp_${PAY_ID}`,
    payment_id: PAY_ID,
    event: { id: EVT_ID, type: "payment.paid", test: false },
    received_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function inboundDetail(
  overrides: Partial<Schemas["InboundWebhookDetail"]> = {},
): Schemas["InboundWebhookDetail"] {
  return {
    ...inboundWebhook(),
    raw_body: '{"id":"inv_123","status":"PAID"}',
    raw_body_truncated: false,
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
    "connections:read",
    "connections:write",
    "audit:read",
    "deliveries:read",
    "deliveries:replay",
    "payments:read",
    "users:manage",
  ],
  admin: [
    "apps:read",
    "apps:write",
    "connections:read",
    "connections:write",
    "audit:read",
    "deliveries:read",
    "deliveries:replay",
    "payments:read",
  ],
  viewer: ["apps:read", "connections:read", "deliveries:read", "payments:read"],
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

/** A provider descriptor shaped like Xendit's, with one field of every type. */
export function provider(overrides: Partial<Schemas["Provider"]> = {}): Schemas["Provider"] {
  return {
    name: "xendit",
    display_name: "Xendit",
    description: "Invoices on your own Xendit account.",
    docs_url: "https://docs.example.com/xendit",
    credential_schema: {
      fields: [
        {
          key: "secret_key",
          type: "string",
          label: "Secret API key",
          description: "Dashboard → Settings → API keys.",
          placeholder: "xnd_development_…",
          secret: true,
          required: true,
          default: null,
          max_length: 255,
          pattern: "^xnd_(development|production)_[A-Za-z0-9]+$",
          pattern_hint: "Starts with xnd_development_ (test) or xnd_production_ (live).",
        },
        {
          key: "callback_token",
          type: "string",
          label: "Webhook verification token",
          description: "Dashboard → Settings → Webhooks.",
          secret: true,
          required: true,
          default: null,
          min_length: 8,
          max_length: 255,
        },
      ],
    },
    settings_schema: {
      fields: [
        {
          key: "invoice_duration",
          type: "integer",
          label: "Invoice lifetime (seconds)",
          description: "Empty uses the provider's default.",
          secret: false,
          required: false,
          default: null,
          minimum: 60,
          maximum: 31536000,
        },
        {
          key: "locale",
          type: "enum",
          label: "Checkout language",
          description: "",
          secret: false,
          required: false,
          default: null,
          options: [
            { value: "id", label: "Indonesian" },
            { value: "en", label: "English" },
          ],
        },
      ],
    },
    capabilities: {
      channels: [
        { code: "QRIS", label: "QRIS" },
        { code: "VA_BCA", label: "BCA Virtual Account" },
        { code: "EWALLET_OVO", label: "OVO" },
      ],
      currencies: ["IDR"],
      can_restrict_channels: true,
      can_cancel: true,
      reports_fee: true,
    },
    webhook_setup: {
      instructions: ["Open Settings → Webhooks.", "Paste the URL into Invoices paid.", "Save."],
      dashboard_url: "https://dashboard.example.com/settings/webhooks",
    },
    ...overrides,
  };
}

export function connectionListItem(
  overrides: Partial<Schemas["ConnectionListItem"]> = {},
): Schemas["ConnectionListItem"] {
  return {
    id: CON_ID,
    name: "xendit-test",
    provider: "xendit",
    environment: "test",
    status: "active",
    legacy_webhook_alias: false,
    last_tested_at: null,
    last_test_status: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

export function connection(overrides: Partial<Schemas["Connection"]> = {}): Schemas["Connection"] {
  return {
    ...connectionListItem(),
    credentials: [
      { key: "secret_key", last4: "a1b2" },
      { key: "callback_token", last4: null },
    ],
    settings: {},
    channels: [
      { channel: "QRIS", label: "QRIS", enabled: true },
      { channel: "VA_BCA", label: "BCA Virtual Account", enabled: true },
      { channel: "EWALLET_OVO", label: "OVO", enabled: true },
    ],
    webhook_url: `https://pay.example.com/webhooks/xendit/${CON_ID}`,
    last_test_error: null,
    ...overrides,
  };
}

/** The `default_connection` an app carries; follows `connection()` unless overridden. */
export function connectionSummary(
  overrides: Partial<Schemas["ConnectionSummary"]> = {},
): Schemas["ConnectionSummary"] {
  const { id, name, provider: providerName, environment, status } = connection();
  return { id, name, provider: providerName, environment, status, ...overrides };
}
