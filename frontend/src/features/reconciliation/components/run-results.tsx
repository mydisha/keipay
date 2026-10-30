import { PanelRightOpenIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { Chip } from "@/shared/ui/chip";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { MoneyText } from "@/shared/ui/money-text";
import { Section } from "@/shared/ui/section";
import { Stat, StatStrip } from "@/shared/ui/stat-strip";
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

/** The run's two totals, then one chip per result with its count; pressing a chip filters the items. */
function ResultCounts({ summary, search }: { summary: RunSummary; search: RunSearch }) {
  return (
    <Section id="results-heading" title={strings.reconciliation.resultsTitle}>
      <StatStrip className="lg:grid-cols-2">
        <Stat label={strings.reconciliation.total} value={summary.total.toLocaleString("id-ID")} />
        <Stat label={strings.reconciliation.unresolved} value={summary.unresolved.toLocaleString("id-ID")} />
      </StatStrip>
      <div className="flex flex-wrap gap-1.5">
        {RECONCILIATION_RESULTS.map((result) => {
          const pressed = search.filters.result === result;
          const count = summary.results[result];
          return (
            <Chip
              key={result}
              pressed={pressed}
              aria-label={`${strings.reconciliation.filterByResult(resultLabel(result))}: ${count}`}
              onClick={() => search.setResult(pressed ? null : result)}
              className="gap-2"
            >
              {resultLabel(result)}
              <span className="tabular-nums">{count.toLocaleString("id-ID")}</span>
            </Chip>
          );
        })}
      </div>
    </Section>
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
    <Section id="items-heading" title={strings.reconciliation.itemsTitle}>
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
    </Section>
  );
}

/** A finished run: counts per result, then its items. */
export function RunResults({ run, search }: { run: ReconciliationRun; search: RunSearch }) {
  if (!run.summary) return null;
  if (run.summary.total === 0) {
    return (
      <EmptyState
        title={strings.reconciliation.nothingTitle}
        description={strings.reconciliation.nothingBody}
      />
    );
  }
  return (
    <>
      <ResultCounts summary={run.summary} search={search} />
      <ItemsTable run={run} search={search} />
    </>
  );
}
