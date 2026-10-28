import { useEffect, useState } from "react";
import { api, LedgerEntry, LedgerSummary } from "../api/client";
import {
  TrendUp,
  CurrencyCircleDollar,
  Receipt,
  Funnel,
  MagnifyingGlass,
  WarningCircle,
} from "@phosphor-icons/react";

export default function Ledger() {
  const [entries, setEntries] = useState<LedgerEntry[]>([]);
  const [summary, setSummary] = useState<LedgerSummary[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [tenantID, setTenantID] = useState("");

  async function load() {
    setError("");
    setLoading(true);
    const params = new URLSearchParams();
    if (tenantID) params.set("tenant_id", tenantID);
    const q = params.toString() ? `?${params.toString()}` : "";
    try {
      const res = await api.listLedger(q);
      setEntries(res.entries || []);
      setSummary(res.summary || []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to load ledger");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    load();
  }, [tenantID]);

  // Calculate totals from summary
  const totals = summary.reduce(
    (acc, s) => ({
      gross: acc.gross + s.gross_amount,
      fees: acc.fees + s.total_fee,
      net: acc.net + s.net_amount,
      count: acc.count + s.count,
    }),
    { gross: 0, fees: 0, net: 0, count: 0 }
  );

  return (
    <div className="space-y-6">
      {/* Page header */}
      <div>
        <h1 className="text-display-md text-ink">Ledger</h1>
        <p className="mt-1 text-body-md text-muted">
          Financial reconciliation and settlement tracking
        </p>
      </div>

      {/* Summary cards */}
      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-4">
        <SummaryCard
          label="Total Transactions"
          value={totals.count.toString()}
          icon={Receipt}
        />
        <SummaryCard
          label="Gross Amount"
          value={formatCurrency(totals.gross)}
          icon={CurrencyCircleDollar}
        />
        <SummaryCard
          label="Total Fees"
          value={formatCurrency(totals.fees)}
          icon={TrendUp}
          variant="warning"
        />
        <SummaryCard
          label="Net Settlement"
          value={formatCurrency(totals.net)}
          icon={CurrencyCircleDollar}
          variant="success"
        />
      </div>

      {/* Filter */}
      <div className="card p-4">
        <div className="flex items-center gap-3">
          <div className="flex items-center gap-2 text-body-md text-muted">
            <Funnel size={16} weight="regular" />
            <span>Filter by:</span>
          </div>
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

      {/* Error */}
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
        ) : entries.length === 0 ? (
          <EmptyState />
        ) : (
          <table className="table">
            <thead>
              <tr>
                <th>Tenant</th>
                <th>Payment</th>
                <th>Gross</th>
                <th>Fee</th>
                <th>Net</th>
                <th>Date</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => (
                <tr key={e.id}>
                  <td>
                    <span className="badge-neutral">#{e.tenant_id}</span>
                  </td>
                  <td>
                    <code className="font-mono text-body-sm text-ink">
                      #{e.payment_id}
                    </code>
                  </td>
                  <td className="font-mono text-ink">
                    {formatCurrency(e.amount)}
                  </td>
                  <td className="font-mono text-status-pending">
                    -{formatCurrency(e.xendit_fee)}
                  </td>
                  <td className="font-mono font-medium text-status-paid">
                    {formatCurrency(e.net_amount)}
                  </td>
                  <td className="text-body-sm text-muted">
                    <RelativeTime date={e.created_at} />
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

function SummaryCard({
  label,
  value,
  icon: Icon,
  variant = "default",
}: {
  label: string;
  value: string;
  icon: any;
  variant?: "default" | "success" | "warning";
}) {
  const variantClasses = {
    default: "bg-surface-soft text-muted",
    success: "bg-green-50 text-status-paid",
    warning: "bg-amber-50 text-status-pending",
  };

  return (
    <div className="card p-5">
      <div className="flex items-start justify-between">
        <div>
          <p className="text-caption text-muted">{label}</p>
          <p className="mt-2 text-display-md text-ink">{value}</p>
        </div>
        <div
          className={`flex h-10 w-10 items-center justify-center rounded-lg ${variantClasses[variant]}`}
        >
          <Icon size={20} weight="fill" />
        </div>
      </div>
    </div>
  );
}

function formatCurrency(amount: number): string {
  return new Intl.NumberFormat("en-US", {
    style: "currency",
    currency: "IDR",
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(amount);
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
            <div className="h-6 w-12 animate-pulse rounded-pill bg-surface-soft" />
            <div className="h-4 w-20 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-24 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-20 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-24 animate-pulse rounded bg-surface-soft" />
            <div className="h-4 w-16 animate-pulse rounded bg-surface-soft" />
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
        <Receipt size={24} weight="regular" className="text-muted" />
      </div>
      <h3 className="text-title-sm text-ink">No ledger entries</h3>
      <p className="mt-2 text-body-md text-muted">
        Settlements will appear here once payments are processed
      </p>
    </div>
  );
}