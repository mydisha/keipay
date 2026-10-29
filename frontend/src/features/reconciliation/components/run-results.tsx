import { CircleCheckBigIcon, PanelRightOpenIcon, SearchXIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  isReconciliationResult,
  RECONCILIATION_RESULTS,
  useRunItems,
  type ReconciliationItem,
  type ReconciliationRun,
} from "../api/runs";
import type { RunSearch } from "../lib/reconciliation-search";
import { ResolutionBadge, ResultBadge, resultLabel } from "./badges";

type RunSummary = NonNullable<ReconciliationRun["summary"]>;

const none = <span className="text-muted-foreground">{strings.common.none}</span>;

function GatewayStatus({ gateway }: { gateway: ReconciliationItem["gateway"] }) {
  if (!gateway) return none;
  // The gateway's own value when it has no KeiPay equivalent.
  if (!gateway.status) return <span className="font-mono text-xs">{gateway.provider_status}</span>;
  return <StatusBadge status={gateway.status} />;
}

function itemColumns(onOpen: (item: ReconciliationItem) => void): DataColumn<ReconciliationItem>[] {
  return [
    {
      id: "reference",
      header: strings.reconciliation.reference,
      card: "title",
      className: "max-w-64",
      cell: (item) => <IdText value={item.reference} label={strings.reconciliation.reference} />,
    },
    { id: "result", header: strings.reconciliation.result, cell: (item) => <ResultBadge result={item.result} /> },
    {
      id: "keipay_amount",
      header: strings.reconciliation.keipayAmount,
      align: "right",
      cell: (item) => (item.keipay ? <MoneyText amount={item.keipay.amount} currency={item.keipay.currency} /> : none),
    },
    {
      id: "gateway_amount",
      header: strings.reconciliation.gatewayAmount,
      align: "right",
      cell: (item) => (item.gateway ? <MoneyText amount={item.gateway.amount} currency={item.gateway.currency} /> : none),
    },
    {
      id: "keipay_status",
      header: strings.reconciliation.keipayStatus,
      cell: (item) => (item.keipay ? <StatusBadge status={item.keipay.status} /> : none),
    },
    { id: "gateway_status", header: strings.reconciliation.gatewayStatus, cell: (item) => <GatewayStatus gateway={item.gateway} /> },
    { id: "resolution", header: strings.reconciliation.resolution, cell: (item) => <ResolutionBadge item={item} /> },
    {
      id: "view",
      header: strings.reconciliation.view,
      card: "actions",
      align: "right",
      cell: (item) => (
        <Button variant="outline" size="sm" onClick={() => onOpen(item)} aria-label={strings.reconciliation.viewItem(item.reference)}>
          <PanelRightOpenIcon aria-hidden="true" />
          {strings.reconciliation.view}
        </Button>
      ),
    },
  ];
}

/** One button per result with its count; pressing one filters the items. */
function ResultCards({ summary, search }: { summary: RunSummary; search: RunSearch }) {
  return (
    <section className="grid gap-3" aria-labelledby="results-heading">
      <h2 id="results-heading" className="text-base font-semibold">
        {strings.reconciliation.resultsTitle}
      </h2>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-4 xl:grid-cols-8">
        <Stat label={strings.reconciliation.total} value={summary.total} />
        <Stat label={strings.reconciliation.unresolved} value={summary.unresolved} />
        {RECONCILIATION_RESULTS.map((result) => {
          const pressed = search.filters.result === result;
          return (
            <button
              key={result}
              type="button"
              aria-pressed={pressed}
              aria-label={`${strings.reconciliation.filterByResult(resultLabel(result))}: ${summary.results[result]}`}
              onClick={() => search.setResult(pressed ? null : result)}
              className={cn(
                "grid gap-1 rounded-lg border bg-card p-3 text-left shadow-xs transition-colors outline-none hover:bg-accent focus-visible:ring-[3px] focus-visible:ring-ring/50",
                pressed && "border-primary ring-1 ring-primary",
              )}
            >
              <ResultBadge result={result} />
              <span className="text-lg font-semibold tabular-nums">{summary.results[result].toLocaleString("id-ID")}</span>
            </button>
          );
        })}
      </div>
    </section>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="grid gap-1 rounded-lg border bg-muted/40 p-3">
      <span className="text-xs text-muted-foreground">{label}</span>
      <span className="text-lg font-semibold tabular-nums">{value.toLocaleString("id-ID")}</span>
    </div>
  );
}

const RESOLVED_ALL = "all";

function ItemsTable({ run, search }: { run: ReconciliationRun; search: RunSearch }) {
  const items = useRunItems(run, search.filters);
  const list = loadMoreList(items);
  const { filters } = search;
  const filtered = filters.result !== null || filters.resolved !== null;
  const resolvedValue = filters.resolved === null ? RESOLVED_ALL : filters.resolved ? "resolved" : "unresolved";

  return (
    <section className="grid gap-3" aria-labelledby="items-heading">
      <h2 id="items-heading" className="text-base font-semibold">
        {strings.reconciliation.itemsTitle}
      </h2>
      <DataTable
        caption={strings.reconciliation.itemsTitle}
        columns={itemColumns((item) => search.openItem(item.id))}
        data={list.rows}
        getRowId={(item) => item.id}
        isLoading={items.isPending}
        error={list.error}
        onRetry={() => void items.refetch()}
        loadMore={list.loadMore}
        onRowClick={(item) => search.openItem(item.id)}
        filters={[
          {
            id: "result",
            label: strings.reconciliation.resultFilter,
            options: RECONCILIATION_RESULTS.map((r) => ({ value: r, label: resultLabel(r) })),
            value: filters.result,
            onChange: (value) => search.setResult(isReconciliationResult(value) ? value : null),
          },
          {
            id: "resolved",
            label: strings.reconciliation.resolvedFilter,
            options: [RESOLVED_ALL, "unresolved", "resolved"].map((v) => ({
              value: v,
              label: strings.reconciliation.resolvedOptions[v] ?? v,
            })),
            value: resolvedValue,
            onChange: (value) => search.setResolved(value === "resolved" || value === "unresolved" ? value : null),
          },
        ]}
        hasActiveFilters={filtered}
        onClearFilters={search.clearFilters}
        emptyState={
          <EmptyState
            icon={SearchXIcon}
            headingLevel={3}
            title={strings.reconciliation.itemsFilteredTitle}
            description={strings.reconciliation.itemsFilteredBody}
            action={
              filtered ? (
                <Button variant="outline" onClick={search.clearFilters}>
                  {strings.common.clearFilters}
                </Button>
              ) : undefined
            }
          />
        }
      />
    </section>
  );
}

/** A finished run: counts per result, then its items. */
export function RunResults({ run, search }: { run: ReconciliationRun; search: RunSearch }) {
  if (!run.summary) return null;
  if (run.summary.total === 0) {
    return (
      <EmptyState
        icon={CircleCheckBigIcon}
        title={strings.reconciliation.nothingTitle}
        description={strings.reconciliation.nothingBody}
      />
    );
  }
  return (
    <>
      <ResultCards summary={run.summary} search={search} />
      <ItemsTable run={run} search={search} />
    </>
  );
}
