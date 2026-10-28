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
