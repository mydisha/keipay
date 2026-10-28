import { FormEvent, useEffect, useState } from "react";
import { api, Credentials, Tenant } from "../api/client";
import {
  Plus,
  ArrowsClockwise,
  Power,
  PencilSimple,
  X,
  CheckCircle,
  WarningCircle,
  Copy,
  Check,
} from "@phosphor-icons/react";

export default function Tenants() {
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [creds, setCreds] = useState<Credentials | null>(null);
  const [editing, setEditing] = useState<Tenant | null>(null);
  const [showCreateForm, setShowCreateForm] = useState(false);
  const [loading, setLoading] = useState(true);
  const [actionLoading, setActionLoading] = useState<number | null>(null);

  // Create form state
  const [name, setName] = useState("");
  const [slug, setSlug] = useState("");
  const [webhookURL, setWebhookURL] = useState("");

  async function load() {
    try {
      setLoading(true);
      const { tenants } = await api.listTenants();
      setTenants(tenants);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load tenants");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, []);

  async function onCreate(e: FormEvent) {
    e.preventDefault();
    setError("");
    setActionLoading(0);
    try {
      const res = await api.createTenant({ name, slug, webhook_url: webhookURL });
      setCreds(res.credentials);
      setName("");
      setSlug("");
      setWebhookURL("");
      setShowCreateForm(false);
      setSuccess("Tenant created successfully");
      await load();
      setTimeout(() => setSuccess(""), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create tenant");
    } finally {
      setActionLoading(null);
    }
  }

  async function onRotate(id: number) {
    if (!confirm("Rotate credentials? The old API key and secret will stop working immediately."))
      return;
    setActionLoading(id);
    try {
      const res = await api.rotateTenant(id);
      setCreds(res.credentials);
      setSuccess("Credentials rotated successfully");
      setTimeout(() => setSuccess(""), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to rotate credentials");
    } finally {
      setActionLoading(null);
    }
  }

  async function onToggleActive(t: Tenant) {
    setActionLoading(t.id);
    try {
      await api.updateTenant(t.id, {
        name: t.name,
        webhook_url: t.webhook_url,
        redirect_success_url: t.redirect_success_url,
        redirect_failure_url: t.redirect_failure_url,
        active: !t.active,
      });
      setSuccess(t.active ? "Tenant disabled" : "Tenant enabled");
      await load();
      setTimeout(() => setSuccess(""), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to update tenant");
    } finally {
      setActionLoading(null);
    }
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-display-md text-ink">Tenants</h1>
          <p className="mt-1 text-body-md text-muted">
            Manage your payment tenants and their credentials
          </p>
        </div>
        <button
          onClick={() => setShowCreateForm(true)}
          className="btn-primary gap-2"
        >
          <Plus size={18} weight="bold" />
          Add Tenant
        </button>
      </div>

      {/* Messages */}
      {error && (
        <div className="flex items-start gap-3 rounded-lg border border-error-bg bg-error-bg px-4 py-3">
          <WarningCircle size={20} weight="fill" className="shrink-0 text-error-text" />
          <span className="text-body-md text-error-text">{error}</span>
        </div>
      )}

      {success && (
        <div className="flex items-start gap-3 rounded-lg border border-green-100 bg-green-50 px-4 py-3">
          <CheckCircle size={20} weight="fill" className="shrink-0 text-status-paid" />
          <span className="text-body-md text-status-paid">{success}</span>
        </div>
      )}

      {/* Credentials callout */}
      {creds && <CredentialsDisplay creds={creds} onDismiss={() => setCreds(null)} />}

      {/* Create form modal */}
      {showCreateForm && (
        <CreateTenantModal
          name={name}
          slug={slug}
          webhookURL={webhookURL}
          setName={setName}
          setSlug={setSlug}
          setWebhookURL={setWebhookURL}
          onClose={() => setShowCreateForm(false)}
          onSubmit={onCreate}
          loading={actionLoading === 0}
        />
      )}

      {/* Table */}
      <div className="table-container">
        {loading ? (
          <TableSkeleton />
        ) : tenants.length === 0 ? (
          <EmptyState onAdd={() => setShowCreateForm(true)} />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Name</th>
                <th>Slug</th>
                <th>API Key</th>
                <th>Status</th>
                <th className="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tenants.map((t) => (
                <tr key={t.id}>
                  <td className="font-medium text-ink">{t.name}</td>
                  <td>
                    <code className="rounded bg-surface-soft px-2 py-1 font-mono text-body-sm text-ink">
                      {t.slug}
                    </code>
                  </td>
                  <td>
                    <code className="font-mono text-body-sm text-muted">
                      {t.api_key.slice(0, 12)}...
                    </code>
                  </td>
                  <td>
                    <span className={t.active ? "badge-success" : "badge-neutral"}>
                      {t.active ? "Active" : "Inactive"}
                    </span>
                  </td>
                  <td className="text-right">
                    <div className="flex items-center justify-end gap-1">
                      <button
                        onClick={() => setEditing(t)}
                        className="btn-ghost px-3 py-2"
                        title="Edit tenant"
                      >
                        <PencilSimple size={16} weight="regular" />
                      </button>
                      <button
                        onClick={() => onRotate(t.id)}
                        disabled={actionLoading === t.id}
                        className="btn-ghost px-3 py-2 text-warning disabled:opacity-50"
                        title="Rotate credentials"
                      >
                        {actionLoading === t.id ? (
                          <span className="spinner-sm" />
                        ) : (
                          <ArrowsClockwise size={16} weight="regular" />
                        )}
                      </button>
                      <button
                        onClick={() => onToggleActive(t)}
                        disabled={actionLoading === t.id}
                        className={`btn-ghost px-3 py-2 disabled:opacity-50 ${
                          t.active ? "text-status-failed" : "text-status-paid"
                        }`}
                        title={t.active ? "Disable tenant" : "Enable tenant"}
                      >
                        {actionLoading === t.id ? (
                          <span className="spinner-sm" />
                        ) : (
                          <Power size={16} weight="regular" />
                        )}
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      {/* Edit modal */}
      {editing && (
        <EditTenantModal
          tenant={editing}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await load();
          }}
          onError={setError}
        />
      )}
    </div>
  );
}

// --- Sub-components ---

function CredentialsDisplay({
  creds,
  onDismiss,
}: {
  creds: Credentials;
  onDismiss: () => void;
}) {
  const [copiedKey, setCopiedKey] = useState(false);
  const [copiedSecret, setCopiedSecret] = useState(false);

  async function copyToClipboard(text: string, type: "key" | "secret") {
    await navigator.clipboard.writeText(text);
    if (type === "key") {
      setCopiedKey(true);
      setTimeout(() => setCopiedKey(false), 2000);
    } else {
      setCopiedSecret(true);
      setTimeout(() => setCopiedSecret(false), 2000);
    }
  }

  return (
    <div className="rounded-lg border border-amber-200 bg-amber-50 p-6">
      <div className="flex items-start justify-between">
        <div className="flex items-start gap-3">
          <WarningCircle size={24} weight="fill" className="shrink-0 text-amber-600" />
          <div>
            <p className="text-label-md text-amber-900">
              Store these credentials now
            </p>
            <p className="mt-1 text-body-md text-amber-700">
              The API secret is shown only once and cannot be retrieved later.
            </p>
          </div>
        </div>
        <button onClick={onDismiss} className="btn-ghost px-2 py-1 text-amber-700">
          <X size={18} weight="regular" />
        </button>
      </div>

      <div className="mt-4 space-y-3 rounded-md bg-white p-4 font-mono text-body-sm">
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0 flex-1">
            <span className="text-muted">api_key: </span>
            <span className="text-ink break-all">{creds.api_key}</span>
          </div>
          <button
            onClick={() => copyToClipboard(creds.api_key, "key")}
            className="btn-ghost shrink-0 px-2 py-1 text-amber-700"
            title="Copy API key"
          >
            {copiedKey ? (
              <Check size={16} weight="bold" />
            ) : (
              <Copy size={16} weight="regular" />
            )}
          </button>
        </div>
        <div className="h-px bg-amber-200" />
        <div className="flex items-center justify-between gap-4">
          <div className="min-w-0 flex-1">
            <span className="text-muted">api_secret: </span>
            <span className="text-ink break-all">{creds.api_secret}</span>
          </div>
          <button
            onClick={() => copyToClipboard(creds.api_secret, "secret")}
            className="btn-ghost shrink-0 px-2 py-1 text-amber-700"
            title="Copy API secret"
          >
            {copiedSecret ? (
              <Check size={16} weight="bold" />
            ) : (
              <Copy size={16} weight="regular" />
            )}
          </button>
        </div>
      </div>
    </div>
  );
}

function CreateTenantModal({
  name,
  slug,
  webhookURL,
  setName,
  setSlug,
  setWebhookURL,
  onClose,
  onSubmit,
  loading,
}: {
  name: string;
  slug: string;
  webhookURL: string;
  setName: (v: string) => void;
  setSlug: (v: string) => void;
  setWebhookURL: (v: string) => void;
  onClose: () => void;
  onSubmit: (e: FormEvent) => void;
  loading: boolean;
}) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between pb-6">
          <h2 className="text-title-lg text-ink">Add new tenant</h2>
          <button onClick={onClose} className="btn-ghost px-2 py-1">
            <X size={20} weight="regular" />
          </button>
        </div>

        <form onSubmit={onSubmit} className="space-y-5">
          <div>
            <label htmlFor="create-name" className="label">
              Name
            </label>
            <input
              id="create-name"
              className="input-field"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="Acme Corp"
              required
            />
          </div>

          <div>
            <label htmlFor="create-slug" className="label">
              Slug
            </label>
            <input
              id="create-slug"
              className="input-field font-mono"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
              placeholder="acme"
              required
            />
            <p className="mt-2 text-body-sm text-muted">
              Used in API routes. Lowercase, no spaces.
            </p>
          </div>

          <div>
            <label htmlFor="create-webhook" className="label">
              Webhook URL <span className="text-muted">(optional)</span>
            </label>
            <input
              id="create-webhook"
              type="url"
              className="input-field"
              value={webhookURL}
              onChange={(e) => setWebhookURL(e.target.value)}
              placeholder="https://api.acme.com/webhooks/kei"
            />
          </div>

          <div className="flex items-center justify-end gap-3 pt-4">
            <button type="button" onClick={onClose} className="btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={loading} className="btn-primary gap-2">
              {loading && <span className="spinner" />}
              Create Tenant
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function EditTenantModal({
  tenant,
  onClose,
  onSaved,
  onError,
}: {
  tenant: Tenant;
  onClose: () => void;
  onSaved: () => void;
  onError: (msg: string) => void;
}) {
  const [name, setName] = useState(tenant.name);
  const [webhookURL, setWebhookURL] = useState(tenant.webhook_url);
  const [successURL, setSuccessURL] = useState(tenant.redirect_success_url);
  const [failureURL, setFailureURL] = useState(tenant.redirect_failure_url);
  const [active, setActive] = useState(tenant.active);
  const [loading, setLoading] = useState(false);

  async function save(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    try {
      await api.updateTenant(tenant.id, {
        name,
        webhook_url: webhookURL,
        redirect_success_url: successURL,
        redirect_failure_url: failureURL,
        active,
      });
      onSaved();
    } catch (err) {
      onError(err instanceof Error ? err.message : "Failed to update tenant");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal-content" onClick={(e) => e.stopPropagation()}>
        <div className="flex items-center justify-between pb-6">
          <h2 className="text-title-lg text-ink">
            Edit <span className="font-mono">{tenant.slug}</span>
          </h2>
          <button onClick={onClose} className="btn-ghost px-2 py-1">
            <X size={20} weight="regular" />
          </button>
        </div>

        <form onSubmit={save} className="space-y-5">
          <div>
            <label htmlFor="edit-name" className="label">
              Name
            </label>
            <input
              id="edit-name"
              className="input-field"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </div>

          <div>
            <label htmlFor="edit-webhook" className="label">
              Webhook URL
            </label>
            <input
              id="edit-webhook"
              type="url"
              className="input-field"
              value={webhookURL}
              onChange={(e) => setWebhookURL(e.target.value)}
              placeholder="https://api.acme.com/webhooks/kei"
            />
          </div>

          <div>
            <label htmlFor="edit-success" className="label">
              Success Redirect URL
            </label>
            <input
              id="edit-success"
              type="url"
              className="input-field"
              value={successURL}
              onChange={(e) => setSuccessURL(e.target.value)}
            />
          </div>

          <div>
            <label htmlFor="edit-failure" className="label">
              Failure Redirect URL
            </label>
            <input
              id="edit-failure"
              type="url"
              className="input-field"
              value={failureURL}
              onChange={(e) => setFailureURL(e.target.value)}
            />
          </div>

          <label className="flex items-center gap-3 rounded-lg border border-hairline p-4 transition-colors hover:bg-surface-soft">
            <input
              type="checkbox"
              checked={active}
              onChange={(e) => setActive(e.target.checked)}
              className="h-4 w-4 rounded border-hairline text-ink focus:ring-link"
            />
            <div>
              <span className="text-label-md text-ink">Active</span>
              <p className="text-body-sm text-muted">
                Inactive tenants cannot process payments
              </p>
            </div>
          </label>

          <div className="flex items-center justify-end gap-3 pt-4">
            <button type="button" onClick={onClose} className="btn-secondary">
              Cancel
            </button>
            <button type="submit" disabled={loading} className="btn-primary gap-2">
              {loading && <span className="spinner" />}
              Save Changes
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}

function TableSkeleton() {
  return (
    <div className="p-6">
      <div className="space-y-3">
        {[1, 2, 3].map((i) => (
          <div key={i} className="flex items-center gap-4">
            <div className="h-4 w-32 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-24 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-48 animate-pulse rounded bg-surface-soft" />
            <div className="h-6 w-16 animate-pulse rounded-pill bg-surface-soft" />
            <div className="ml-auto h-8 w-24 animate-pulse rounded bg-surface-soft" />
          </div>
        ))}
      </div>
    </div>
  );
}

function EmptyState({ onAdd }: { onAdd: () => void }) {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-soft">
        <Plus size={24} weight="regular" className="text-muted" />
      </div>
      <h3 className="text-title-sm text-ink">No tenants yet</h3>
      <p className="mt-2 text-body-md text-muted">
        Create your first tenant to start processing payments
      </p>
      <button onClick={onAdd} className="btn-primary mt-6 gap-2">
        <Plus size={18} weight="bold" />
        Add Tenant
      </button>
    </div>
  );
}