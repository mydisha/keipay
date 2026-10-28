import { useEffect, useState } from "react";
import { api, Delivery } from "../api/client";
import {
  CheckCircle,
  XCircle,
  Clock,
  ArrowsClockwise,
  Funnel,
  MagnifyingGlass,
  WarningCircle,
  WebhooksLogo,
} from "@phosphor-icons/react";

const statuses = ["", "PENDING", "SUCCESS", "FAILED"];

export default function Deliveries() {
  const [deliveries, setDeliveries] = useState<Delivery[]>([]);
  const [error, setError] = useState("");
  const [success, setSuccess] = useState("");
  const [status, setStatus] = useState("");
  const [tenantID, setTenantID] = useState("");
  const [loading, setLoading] = useState(true);
  const [replaying, setReplaying] = useState<number | null>(null);

  async function load() {
    setError("");
    setLoading(true);
    const params = new URLSearchParams();
    if (status) params.set("status", status);
    if (tenantID) params.set("tenant_id", tenantID);
    const q = params.toString() ? `?${params.toString()}` : "";
    try {
      const res = await api.listDeliveries(q);
      setDeliveries(res.deliveries || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load deliveries");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [status, tenantID]);

  async function onReplay(id: number) {
    setReplaying(id);
    try {
      await api.replayDelivery(id);
      setSuccess("Delivery requeued successfully");
      await load();
      setTimeout(() => setSuccess(""), 3000);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to replay delivery");
    } finally {
      setReplaying(null);
    }
  }

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div>
        <h1 className="text-display-md text-ink">Webhook Deliveries</h1>
        <p className="mt-1 text-body-md text-muted">
          Monitor and replay webhook delivery attempts
        </p>
      </div>

      {/* Filters */}
      <div className="card p-4">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2 text-body-md text-muted">
            <Funnel size={16} weight="regular" />
            <span>Filter by:</span>
          </div>

          <select
            value={status}
            onChange={(e) => setStatus(e.target.value)}
            className="input-field"
            style={{ minWidth: "140px" }}
          >
            {statuses.map((s) => (
              <option key={s} value={s}>
                {s || "All statuses"}
              </option>
            ))}
          </select>

          <div className="relative">
            <input
              placeholder="Tenant ID"
              value={tenantID}
              onChange={(e) => setTenantID(e.target.value)}
              className="input-field pl-10"
              style={{ width: "140px" }}
            />
            <MagnifyingGlass
              size={16}
              weight="regular"
              className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-muted"
            />
          </div>
        </div>
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

      {/* Table */}
      <div className="table-container">
        {loading ? (
          <TableSkeleton />
        ) : deliveries.length === 0 ? (
          <EmptyState />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Payment</th>
                <th>Tenant</th>
                <th>Event</th>
                <th>Status</th>
                <th>Attempts</th>
                <th>Error</th>
                <th>Created</th>
                <th className="text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {deliveries.map((d) => (
                <tr key={d.id}>
                  <td>
                    <code className="font-mono text-body-sm text-ink">
                      #{d.payment_id}
                    </code>
                  </td>
                  <td>
                    <span className="badge-neutral">#{d.tenant_id}</span>
                  </td>
                  <td className="text-ink">
                    <code className="rounded bg-surface-soft px-2 py-0.5 font-mono text-body-sm">
                      {d.event_type}
                    </code>
                  </td>
                  <td>
                    <StatusBadge status={d.status} />
                  </td>
                  <td>
                    <span className="font-mono text-body-sm">
                      {d.attempts}
                    </span>
                  </td>
                  <td className="max-w-xs">
                    {d.last_error ? (
                      <span
                        className="block truncate text-body-sm text-status-failed"
                        title={d.last_error}
                      >
                        {d.last_error}
                      </span>
                    ) : (
                      <span className="text-muted">-</span>
                    )}
                  </td>
                  <td className="text-body-sm text-muted">
                    <RelativeTime date={d.created_at} />
                  </td>
                  <td className="text-right">
                    <button
                      onClick={() => onReplay(d.id)}
                      disabled={replaying === d.id}
                      className="btn-ghost px-3 py-2 text-link disabled:opacity-50"
                      title="Replay this delivery"
                    >
                      {replaying === d.id ? (
                        <span className="spinner-sm" />
                      ) : (
                        <ArrowsClockwise size={16} weight="regular" />
                      )}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

function StatusBadge({ status }: { status: string }) {
  const config = {
    SUCCESS: {
      className: "badge-success",
      icon: CheckCircle,
      label: "Success",
    },
    PENDING: {
      className: "badge-warning",
      icon: Clock,
      label: "Pending",
    },
    FAILED: {
      className: "badge-error",
      icon: XCircle,
      label: "Failed",
    },
  };

  const { className, icon: Icon, label } = config[status as keyof typeof config] || {
    className: "badge-neutral",
    icon: Clock,
    label: status,
  };

  return (
    <span className={className}>
      <Icon size={14} weight="fill" />
      {label}
    </span>
  );
}

function RelativeTime({ date }: { date: string }) {
  const [relative, setRelative] = useState("");

  useEffect(() => {
    function update() {
      const now = Date.now();
      const then = new Date(date).getTime();
      const diff = now - then;

      const seconds = Math.floor(diff / 1000);
      const minutes = Math.floor(seconds / 60);
      const hours = Math.floor(minutes / 60);
      const days = Math.floor(hours / 24);

      if (days > 7) {
        setRelative(new Date(date).toLocaleDateString());
      } else if (days > 0) {
        setRelative(`${days}d ago`);
      } else if (hours > 0) {
        setRelative(`${hours}h ago`);
      } else if (minutes > 0) {
        setRelative(`${minutes}m ago`);
      } else {
        setRelative("Just now");
      }
    }

    update();
    const interval = setInterval(update, 60000);
    return () => clearInterval(interval);
  }, [date]);

  return (
    <time dateTime={date} title={new Date(date).toLocaleString()}>
      {relative}
    </time>
  );
}

function TableSkeleton() {
  return (
    <div className="p-6">
      <div className="space-y-4">
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="flex items-center gap-4">
            <div className="h-4 w-20 animate-pulse rounded bg-surface-soft" />
            <div className="h-6 w-12 animate-pulse rounded-pill bg-surface-soft" />
            <div className="h-4 w-24 animate-pulse rounded bg-surface-soft" />
            <div className="h-6 w-16 animate-pulse rounded-pill bg-surface-soft" />
            <div className="h-4 w-8 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-32 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-16 animate-pulse rounded bg-surface-soft" />
            <div className="ml-auto h-8 w-8 animate-pulse rounded bg-surface-soft" />
          </div>
        ))}
      </div>
    </div>
  );
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16">
      <div className="mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-surface-soft">
        <WebhooksLogo size={24} weight="regular" className="text-muted" />
      </div>
      <h3 className="text-title-sm text-ink">No webhook deliveries</h3>
      <p className="mt-2 text-body-md text-muted">
        Webhook events will appear here once payments are processed
      </p>
    </div>
  );
}