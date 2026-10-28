import { useEffect, useState } from "react";
import { api, Payment } from "../api/client";
import {
  CheckCircle,
  Clock,
  WarningCircle,
  Timer,
  ArrowSquareOut,
  MagnifyingGlass,
  Funnel,
} from "@phosphor-icons/react";

const statuses = ["", "PENDING", "PAID", "EXPIRED", "FAILED"];

export default function Payments() {
  const [payments, setPayments] = useState<Payment[]>([]);
  const [total, setTotal] = useState(0);
  const [status, setStatus] = useState("");
  const [tenantID, setTenantID] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);

  async function load() {
    setError("");
    setLoading(true);
    const params = new URLSearchParams();
    if (status) params.set("status", status);
    if (tenantID) params.set("tenant_id", tenantID);
    const q = params.toString() ? `?${params.toString()}` : "";
    try {
      const res = await api.listPayments(q);
      setPayments(res.payments || []);
      setTotal(res.total);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load payments");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [status, tenantID]);

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div>
        <div className="flex items-baseline gap-3">
          <h1 className="text-display-md text-ink">Payments</h1>
          <span className="text-title-md text-muted">({total})</span>
        </div>
        <p className="mt-1 text-body-md text-muted">
          View and filter payment transactions across all tenants
        </p>
      </div>

      {/* Filters */}
      <div className="card p-4">
        <div className="flex flex-wrap items-center gap-3">
          <div className="flex items-center gap-2 text-body-md text-muted">
            <Funnel size={16} weight="regular" />
            <span>Filter by:</span>
          </div>

          <div className="relative">
            <select
              value={status}
              onChange={(e) => setStatus(e.target.value)}
              className="input-field appearance-none pr-10"
              style={{ minWidth: "140px" }}
            >
              {statuses.map((s) => (
                <option key={s} value={s}>
                  {s || "All statuses"}
                </option>
              ))}
            </select>
            <MagnifyingGlass
              size={16}
              weight="regular"
              className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-muted"
            />
          </div>

          <input
            placeholder="Tenant ID"
            value={tenantID}
            onChange={(e) => setTenantID(e.target.value)}
            className="input-field"
            style={{ width: "120px" }}
          />
        </div>
      </div>

      {/* Error message */}
      {error && (
        <div className="flex items-start gap-3 rounded-lg border border-error-bg bg-error-bg px-4 py-3">
          <WarningCircle size={20} weight="fill" className="shrink-0 text-error-text" />
          <span className="text-body-md text-error-text">{error}</span>
        </div>
      )}

      {/* Table */}
      <div className="table-container">
        {loading ? (
          <TableSkeleton />
        ) : payments.length === 0 ? (
          <EmptyState />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Order ID</th>
                <th>Tenant</th>
                <th>Amount</th>
                <th>Status</th>
                <th>Created</th>
                <th>Invoice</th>
              </tr>
            </thead>
            <tbody>
              {payments.map((p) => (
                <tr key={p.id}>
                  <td className="font-medium text-ink">
                    <code className="font-mono text-body-sm">{p.order_id}</code>
                  </td>
                  <td>
                    <span className="badge-neutral">#{p.tenant_id}</span>
                  </td>
                  <td className="font-mono text-ink">
                    {p.currency} {p.amount.toLocaleString()}
                  </td>
                  <td>
                    <StatusBadge status={p.status} />
                  </td>
                  <td className="text-body-sm text-muted">
                    <RelativeTime date={p.created_at} />
                  </td>
                  <td>
                    {p.invoice_url ? (
                      <a
                        href={p.invoice_url}
                        target="_blank"
                        rel="noopener noreferrer"
                        className="link inline-flex items-center gap-1"
                      >
                        <span>View</span>
                        <ArrowSquareOut size={14} weight="regular" />
                      </a>
                    ) : (
                      <span className="text-muted">-</span>
                    )}
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
    PAID: {
      className: "badge-success",
      icon: CheckCircle,
      label: "Paid",
    },
    PENDING: {
      className: "badge-warning",
      icon: Clock,
      label: "Pending",
    },
    FAILED: {
      className: "badge-error",
      icon: WarningCircle,
      label: "Failed",
    },
    EXPIRED: {
      className: "badge-neutral",
      icon: Timer,
      label: "Expired",
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

  return <time dateTime={date} title={new Date(date).toLocaleString()}>{relative}</time>;
}

function TableSkeleton() {
  return (
    <div className="p-6">
      <div className="space-y-4">
        {[1, 2, 3, 4, 5].map((i) => (
          <div key={i} className="flex items-center gap-4">
            <div className="h-4 w-32 animate-pulse rounded bg-surface-soft" />
            <div className="h-6 w-12 animate-pulse rounded-pill bg-surface-soft" />
            <div className="h-4 w-24 animate-pulse rounded bg-surface-soft" />
            <div className="h-6 w-16 animate-pulse rounded-pill bg-surface-soft" />
            <div className="h-4 w-20 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-12 animate-pulse rounded bg-surface-soft" />
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
        <Clock size={24} weight="regular" className="text-muted" />
      </div>
      <h3 className="text-title-sm text-ink">No payments found</h3>
      <p className="mt-2 text-body-md text-muted">
        Try adjusting your filters or check back later
      </p>
    </div>
  );
}