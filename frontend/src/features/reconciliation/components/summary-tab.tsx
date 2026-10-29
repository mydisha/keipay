import { CalendarRangeIcon, DownloadIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link } from "react-router";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppLabel } from "@/features/apps/components/app-label";
import { useConnectionFilterOptions } from "@/features/connections/api/connections";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { wibRange } from "@/shared/lib/wib-dates";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn, type FilterChipGroup } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  reportsExportUrl,
  usePaymentReports,
  usePaymentReportSummary,
  type PaymentReport,
  type ReportFilters,
  type ReportInterval,
  type SummaryRow,
} from "../api/reports";
import { formatPeriod } from "../lib/format-range";
import { SUMMARY_FILTER_KEYS, type ReconciliationSearch } from "../lib/reconciliation-search";
import { serverRangeError, summaryRangeError } from "../lib/summary-range";
import { DateRangeFilter, IntervalControl } from "./period-controls";

/** The summary and list filters of a range the server accepts. */
function summaryFilters(search: ReconciliationSearch, environment: ReportFilters["environment"]): ReportFilters {
  return {
    ...wibRange(search.range.from, search.range.to),
    environment,
    appId: search.appId,
    connectionId: search.connectionId,
    currency: search.currency,
  };
}

function ConnectionName({ connection }: { connection: SummaryRow["connection"] }) {
  if (!connection) return <span className="text-muted-foreground">{strings.common.none}</span>;
  return (
    <span className="block truncate font-mono text-[0.8125rem]" title={connection.name}>
      {connection.name}
    </span>
  );
}

function FeeCell({ row }: { row: SummaryRow }) {
  return (
    <span className="grid justify-items-end gap-0.5">
      <MoneyText amount={row.fee} currency={row.currency} />
      {row.fee_unknown_count > 0 && (
        <span className="text-xs whitespace-nowrap text-muted-foreground" title={strings.reconciliation.feeUnknownHint}>
          {strings.reconciliation.feeUnknown(row.fee_unknown_count)}
        </span>
      )}
    </span>
  );
}

function summaryColumns(interval: ReportInterval): DataColumn<SummaryRow>[] {
  return [
    {
      id: "period",
      header: strings.reconciliation.period,
      card: "title",
      cell: (row) => <span className="whitespace-nowrap">{formatPeriod(row, interval)}</span>,
    },
    { id: "app", header: strings.reconciliation.app, className: "max-w-56", cell: (row) => <AppLabel app={row.app} /> },
    {
      id: "connection",
      header: strings.reconciliation.connection,
      className: "max-w-48",
      cell: (row) => <ConnectionName connection={row.connection} />,
    },
    { id: "currency", header: strings.reconciliation.currency, cell: (row) => <span className="font-mono">{row.currency}</span> },
    {
      id: "count",
      header: strings.reconciliation.count,
      align: "right",
      cell: (row) => <span className="tabular-nums">{row.count.toLocaleString("id-ID")}</span>,
    },
    {
      id: "gross",
      header: strings.reconciliation.gross,
      align: "right",
      cell: (row) => <MoneyText amount={row.gross} currency={row.currency} />,
    },
    { id: "fee", header: strings.reconciliation.fee, align: "right", cell: (row) => <FeeCell row={row} /> },
    {
      id: "net",
      header: strings.reconciliation.net,
      align: "right",
      cell: (row) => <MoneyText amount={row.net} currency={row.currency} className="font-medium" />,
    },
  ];
}

function OptionalMoney({ amount, currency }: { amount: number | null; currency: string }) {
  if (amount === null) return <span className="block text-right text-muted-foreground">{strings.common.none}</span>;
  return <MoneyText amount={amount} currency={currency} />;
}

const reportColumns: DataColumn<PaymentReport>[] = [
  {
    id: "payment",
    header: strings.reconciliation.payment,
    card: "title",
    className: "max-w-60",
    cell: (r) => <IdText value={r.payment_id} label={strings.reconciliation.payment} />,
  },
  { id: "app", header: strings.reconciliation.app, className: "max-w-56", cell: (r) => <AppLabel app={r.app} /> },
  {
    id: "connection",
    header: strings.reconciliation.connection,
    className: "max-w-48",
    cell: (r) => <ConnectionName connection={r.connection} />,
  },
  { id: "status", header: strings.reconciliation.status, cell: (r) => <StatusBadge status={r.status} /> },
  {
    id: "gross",
    header: strings.reconciliation.gross,
    align: "right",
    cell: (r) => <MoneyText amount={r.gross} currency={r.currency} />,
  },
  { id: "fee", header: strings.reconciliation.fee, align: "right", cell: (r) => <OptionalMoney amount={r.fee} currency={r.currency} /> },
  { id: "net", header: strings.reconciliation.net, align: "right", cell: (r) => <OptionalMoney amount={r.net} currency={r.currency} /> },
  { id: "paid", header: strings.reconciliation.paidAt, cell: (r) => <DateTimeText value={r.paid_at} /> },
];

const keyToField = { app_id: "appId", connection_id: "connectionId", currency: "currency" } as const;

const summaryRowId = (row: SummaryRow) => [row.period_start, row.app.id, row.connection?.id ?? "", row.currency].join("|");

/** Currencies seen in the totals; the chip group shows only when there is a choice. */
function useCurrencyOptions(rows: SummaryRow[] | undefined, selected: string | null): string[] {
  // While one currency is chosen the totals only show that one, so the set
  // seen without a choice is kept. Updating state during render is React's
  // pattern for state derived from props: it re-renders before committing.
  const [known, setKnown] = useState<string[]>([]);
  if (rows && selected === null) {
    const seen = [...new Set(rows.map((row) => row.currency))].sort();
    if (seen.join() !== known.join()) setKnown(seen);
  }
  const options = new Set(known);
  if (selected) options.add(selected);
  return [...options].sort();
}

export function SummaryTab({ search }: { search: ReconciliationSearch }) {
  const { mode } = useMode();
  const localRangeProblem = summaryRangeError(search.range, search.interval);
  const filters = localRangeProblem ? null : summaryFilters(search, mode);
  const summary = usePaymentReportSummary(filters, search.interval);
  const rangeProblem = localRangeProblem ?? serverRangeError(summary.error, search.interval);
  const blocked = rangeProblem !== null;
  const rows = blocked ? [] : summary.data?.data;
  const isEmpty = !blocked && rows !== undefined && rows.length === 0;

  const connections = useConnectionFilterOptions();
  const connectionOptions = (connections.data ?? []).filter((c) => c.environment === mode);
  const currencies = useCurrencyOptions(summary.data?.data, search.currency);

  // A connection belongs to one mode; switching modes drops its filter.
  const lastMode = useRef(mode);
  const { setFilter } = search;
  useEffect(() => {
    if (lastMode.current === mode) return;
    lastMode.current = mode;
    setFilter("connection_id", null);
  }, [mode, setFilter]);

  const filtered = SUMMARY_FILTER_KEYS.some((key) => search[keyToField[key]] !== null);
  const clearFilters = () => search.clearFilters(SUMMARY_FILTER_KEYS);

  const chips: FilterChipGroup[] = [];
  if (connectionOptions.length > 0) {
    chips.push({
      id: "connection",
      label: strings.reconciliation.connectionFilter,
      options: connectionOptions.map((c) => ({ value: c.id, label: c.name })),
      value: search.connectionId,
      onChange: (value) => search.setFilter("connection_id", value),
    });
  }
  if (currencies.length > 1 || search.currency !== null) {
    chips.push({
      id: "currency",
      label: strings.reconciliation.currencyFilter,
      options: currencies.map((c) => ({ value: c, label: c })),
      value: search.currency,
      onChange: (value) => search.setFilter("currency", value),
    });
  }

  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-end justify-between gap-x-6 gap-y-3">
        <DateRangeFilter range={search.range} onChange={search.setRange} now={search.opened} error={rangeProblem} />
        <div className="flex flex-wrap items-center gap-3">
          <IntervalControl value={search.interval} onChange={search.setInterval} />
          {filters && (
            <Button asChild variant="outline" size="sm">
              <a href={reportsExportUrl(filters)} download>
                <DownloadIcon aria-hidden="true" />
                {strings.reconciliation.exportCsv}
              </a>
            </Button>
          )}
        </div>
      </div>

      <section className="grid gap-3" aria-labelledby="summary-heading">
        <h2 id="summary-heading" className="text-base font-semibold">
          {strings.reconciliation.summaryTitle}
        </h2>
        <DataTable
          caption={strings.reconciliation.summaryTitle}
          columns={summaryColumns(search.interval)}
          data={rows}
          getRowId={summaryRowId}
          isLoading={!blocked && summary.isPending}
          error={blocked ? null : summary.error}
          onRetry={() => void summary.refetch()}
          filters={chips}
          toolbar={<AppFilterSelect value={search.appId} onChange={(appId) => search.setFilter("app_id", appId)} />}
          hasActiveFilters={filtered}
          onClearFilters={clearFilters}
          skeletonRows={3}
          emptyState={
            blocked ? (
              <EmptyState icon={CalendarRangeIcon} headingLevel={3} title={strings.reconciliation.rangeBlocked} />
            ) : (
              <EmptyState
                icon={CalendarRangeIcon}
                headingLevel={3}
                title={strings.reconciliation.emptyTitle}
                description={strings.reconciliation.emptyBody}
                action={
                  filtered ? (
                    <Button variant="outline" onClick={clearFilters}>
                      {strings.common.clearFilters}
                    </Button>
                  ) : (
                    <Button asChild variant="outline">
                      <Link to="/payments">{strings.reconciliation.viewPayments}</Link>
                    </Button>
                  )
                }
              />
            )
          }
        />
      </section>

      {filters && !isEmpty && !summary.error && <ReportList filters={filters} />}
    </div>
  );
}

function ReportList({ filters }: { filters: ReportFilters }) {
  const reports = usePaymentReports(filters);
  const list = loadMoreList(reports);
  return (
    <section className="grid gap-3" aria-labelledby="reports-heading">
      <h2 id="reports-heading" className="text-base font-semibold">
        {strings.reconciliation.reportsTitle}
      </h2>
      <DataTable
        caption={strings.reconciliation.reportsTitle}
        columns={reportColumns}
        data={list.rows}
        getRowId={(r) => r.payment_id}
        isLoading={reports.isPending}
        error={list.error}
        onRetry={() => void reports.refetch()}
        loadMore={list.loadMore}
        emptyState={<EmptyState headingLevel={3} title={strings.reconciliation.emptyTitle} />}
      />
    </section>
  );
}
