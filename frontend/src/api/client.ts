// Minimal fetch wrapper for the admin API. Stores JWT in localStorage.

const TOKEN_KEY = "kei_admin_token";

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string) {
  localStorage.setItem(TOKEN_KEY, token);
}

export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const token = getToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;

  const res = await fetch(path, {
    method,
    headers,
    body: body ? JSON.stringify(body) : undefined,
  });

  const text = await res.text();
  const data = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new ApiError(res.status, data.error || res.statusText);
  }
  return data as T;
}

export interface LoginResult {
  token: string;
  must_change_password: boolean;
}

export const api = {
  login: (username: string, password: string) =>
    request<LoginResult>("POST", "/admin/login", { username, password }),
  changePassword: (currentPassword: string, newPassword: string) =>
    request<{ message: string }>("POST", "/admin/change-password", {
      current_password: currentPassword,
      new_password: newPassword,
    }),
  listTenants: () => request<{ tenants: Tenant[] }>("GET", "/admin/tenants"),
  createTenant: (input: CreateTenantInput) =>
    request<{ tenant: Tenant; credentials: Credentials }>("POST", "/admin/tenants", input),
  updateTenant: (id: number, input: UpdateTenantInput) =>
    request<{ tenant: Tenant }>("PUT", `/admin/tenants/${id}`, input),
  rotateTenant: (id: number) =>
    request<{ tenant: Tenant; credentials: Credentials }>("POST", `/admin/tenants/${id}/rotate`),
  listPayments: (q = "") =>
    request<{ payments: Payment[]; total: number }>("GET", `/admin/payments${q}`),
  listLedger: (q = "") =>
    request<{ entries: LedgerEntry[]; summary: LedgerSummary[] }>("GET", `/admin/ledger${q}`),
  listDeliveries: (q = "") =>
    request<{ deliveries: Delivery[] }>("GET", `/admin/webhook-deliveries${q}`),
  replayDelivery: (id: number) =>
    request<{ requeued: boolean }>("POST", `/admin/webhook-deliveries/${id}/replay`),
};

export interface Tenant {
  id: number;
  name: string;
  slug: string;
  api_key: string;
  webhook_url: string;
  redirect_success_url: string;
  redirect_failure_url: string;
  active: boolean;
}

export interface Credentials {
  api_key: string;
  api_secret: string;
}

export interface CreateTenantInput {
  name: string;
  slug: string;
  webhook_url?: string;
  redirect_success_url?: string;
  redirect_failure_url?: string;
}

export interface UpdateTenantInput {
  name: string;
  webhook_url: string;
  redirect_success_url: string;
  redirect_failure_url: string;
  active: boolean;
}

export interface Payment {
  id: number;
  tenant_id: number;
  order_id: string;
  status: string;
  amount: number;
  currency: string;
  invoice_url: string;
  created_at: string;
}

export interface LedgerEntry {
  id: number;
  tenant_id: number;
  payment_id: number;
  amount: number;
  xendit_fee: number;
  net_amount: number;
  created_at: string;
}

export interface LedgerSummary {
  tenant_id: number;
  count: number;
  gross_amount: number;
  total_fee: number;
  net_amount: number;
}

export interface Delivery {
  id: number;
  payment_id: number;
  tenant_id: number;
  event_type: string;
  status: string;
  attempts: number;
  last_error: string;
  created_at: string;
}
