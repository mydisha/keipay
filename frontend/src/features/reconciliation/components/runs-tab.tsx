import { PanelRightOpenIcon, PlayIcon, ScaleIcon } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { usePermission } from "@/features/auth/components/can";
import { EnvironmentBadge } from "@/features/apps/components/environment-badge";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { isRunStatus, RUN_STATUSES, useReconciliationRuns, type ReconciliationRun } from "../api/runs";
import { formatRunRange } from "../lib/format-range";
import { runPath, type ReconciliationSearch } from "../lib/reconciliation-search";
import { RunStatusBadge } from "./badges";
import { RunDialog } from "./run-dialog";

export function RunConnection({ run }: { run: Pick<ReconciliationRun, "connection"> }) {
  return (
    <span className="inline-flex max-w-full min-w-0 items-center gap-2">
      <span className="truncate font-mono text-[0.8125rem]" title={run.connection.name}>
        {run.connection.name}
      </span>
      <EnvironmentBadge environment={run.connection.environment} />
    </span>
  );
}

function UnresolvedCount({ run }: { run: ReconciliationRun }) {
  if (!run.summary) return <span className="text-muted-foreground">{strings.common.none}</span>;
  return <span className="tabular-nums">{run.summary.unresolved.toLocaleString("id-ID")}</span>;
}

const columns: DataColumn<ReconciliationRun>[] = [
  {
    id: "id",
    header: strings.reconciliation.run,
    card: "title",
    className: "max-w-60",
    cell: (run) => <IdText value={run.id} label={strings.reconciliation.runId} />,
  },
  { id: "connection", header: strings.reconciliation.connection, className: "max-w-56", cell: (run) => <RunConnection run={run} /> },
  {
    id: "range",
    header: strings.reconciliation.range,
    cell: (run) => <span className="whitespace-nowrap">{formatRunRange(run.from, run.to)}</span>,
  },
  { id: "status", header: strings.reconciliation.status, cell: (run) => <RunStatusBadge status={run.status} /> },
  { id: "unresolved", header: strings.reconciliation.unresolved, align: "right", cell: (run) => <UnresolvedCount run={run} /> },
  {
    id: "by",
    header: strings.reconciliation.startedBy,
    className: "max-w-40",
    cell: (run) => (
      <span className="block truncate" title={run.created_by?.name}>
        {run.created_by?.name ?? strings.reconciliation.system}
      </span>
    ),
  },
  { id: "created", header: strings.reconciliation.created, cell: (run) => <DateTimeText value={run.created_at} /> },
  {
    id: "view",
    header: strings.reconciliation.view,
    card: "actions",
    align: "right",
    cell: (run) => (
      <Button asChild variant="outline" size="sm">
        <Link to={runPath(run.id)} aria-label={strings.reconciliation.viewRun(run.id)}>
          <PanelRightOpenIcon aria-hidden="true" />
          {strings.reconciliation.view}
        </Link>
      </Button>
    ),
  },
];

/** The "Run reconciliation" button and its dialog. */
export function RunButton({ variant = "default" }: { variant?: "default" | "outline" }) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant={variant} onClick={() => setOpen(true)}>
        <PlayIcon aria-hidden="true" />
        {strings.reconciliation.runReconciliation}
      </Button>
      <RunDialog open={open} onOpenChange={setOpen} />
    </>
  );
}

export function RunsTab({ search }: { search: ReconciliationSearch }) {
  const { mode } = useMode();
  const navigate = useNavigate();
  const canWrite = usePermission("reconciliation:write");
  const runs = useReconciliationRuns({ status: search.status, environment: mode });
  const list = loadMoreList(runs);
  const filtered = search.status !== null;
  const clearFilters = () => search.setFilter("status", null);

  return (
    <section className="grid gap-3" aria-labelledby="runs-heading">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="runs-heading" className="text-base font-semibold">
          {strings.reconciliation.runsTitle}
        </h2>
        {canWrite && <RunButton />}
      </div>
      <DataTable
        caption={strings.reconciliation.runsTitle}
        columns={columns}
        data={list.rows}
        getRowId={(run) => run.id}
        isLoading={runs.isPending}
        error={list.error}
        onRetry={() => void runs.refetch()}
        loadMore={list.loadMore}
        onRowClick={(run) => void navigate(runPath(run.id))}
        filters={[
          {
            id: "status",
            label: strings.reconciliation.statusFilter,
            options: RUN_STATUSES.map((s) => ({ value: s, label: strings.reconciliation.runStatus[s] ?? s })),
            value: search.status,
            onChange: (value) => search.setFilter("status", isRunStatus(value) ? value : null),
          },
        ]}
        hasActiveFilters={filtered}
        onClearFilters={clearFilters}
        emptyState={
          filtered ? (
            <EmptyState
              icon={ScaleIcon}
              headingLevel={3}
              title={strings.reconciliation.runsFilteredTitle}
              description={strings.reconciliation.runsFilteredBody}
              action={
                <Button variant="outline" onClick={clearFilters}>
                  {strings.common.clearFilters}
                </Button>
              }
            />
          ) : (
            <EmptyState
              icon={ScaleIcon}
              headingLevel={3}
              title={strings.reconciliation.runsEmptyTitle}
              description={
                canWrite
                  ? strings.reconciliation.runsEmptyBody
                  : `${strings.reconciliation.runsEmptyBody} ${strings.reconciliation.runsEmptyReadOnly}`
              }
              action={canWrite ? <RunButton variant="outline" /> : undefined}
            />
          )
        }
      />
    </section>
  );
}
