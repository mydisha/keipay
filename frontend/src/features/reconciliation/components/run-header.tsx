import { ChevronLeftIcon, DownloadIcon, RotateCwIcon } from "lucide-react";
import { useState, type ReactNode } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { CopyButton } from "@/shared/ui/copy-button";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { Skeleton } from "@/shared/ui/skeleton";
import { useRerun, type ReconciliationRun } from "../api/runs";
import { formatRunRange } from "../lib/format-range";
import { RUNS_TAB_PATH } from "../lib/reconciliation-search";
import { RunStatusBadge } from "./badges";
import { RunConnection } from "./runs-tab";

export function BackToRuns() {
  return (
    <Link
      to={RUNS_TAB_PATH}
      className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11"
    >
      <ChevronLeftIcon className="size-4" aria-hidden="true" />
      {strings.reconciliation.backToRuns}
    </Link>
  );
}

/** The toast for a failed re-run: `409 run_in_progress` says why. */
export function rerunErrorMessage(error: unknown): string {
  if (isApiError(error) && error.code === "run_in_progress") return strings.reconciliation.runInProgress;
  return errorMessage(error, strings.reconciliation.rerunFailed);
}

export function RerunButton({ run }: { run: ReconciliationRun }) {
  const rerun = useRerun();
  const [confirming, setConfirming] = useState(false);
  return (
    <>
      <Button variant="outline" onClick={() => setConfirming(true)}>
        <RotateCwIcon aria-hidden="true" />
        {strings.reconciliation.rerun}
      </Button>
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={strings.reconciliation.rerunTitle}
        description={strings.reconciliation.rerunBody}
        confirmLabel={strings.reconciliation.rerun}
        pendingLabel={strings.reconciliation.rerunning}
        pending={rerun.isPending}
        onConfirm={() =>
          rerun.mutate(run.id, {
            onSuccess: () => {
              setConfirming(false);
              toast.success(strings.reconciliation.rerun_toast);
            },
            onError: (error) => {
              setConfirming(false);
              toast.error(rerunErrorMessage(error));
            },
          })
        }
      />
    </>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 gap-1">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm">{children}</dd>
    </div>
  );
}

/** Back link, the run id as the title, its status, key facts and the actions. */
export function RunHeader({ run, exportHref }: { run: ReconciliationRun; exportHref: string | null }) {
  const canWrite = usePermission("reconciliation:write");
  return (
    <div className="grid gap-4">
      <BackToRuns />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-1">
          <p className="text-sm text-muted-foreground">{strings.reconciliation.runTitle}</p>
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h1 className="min-w-0 truncate font-mono text-xl font-semibold tracking-tight" title={run.id}>
              {run.id}
            </h1>
            <CopyButton value={run.id} label={`${strings.common.copyValue(strings.reconciliation.runId)} ${run.id}`} />
            <RunStatusBadge status={run.status} />
          </div>
        </div>
        {(exportHref || canWrite) && (
          <div className="flex flex-wrap gap-2">
            {exportHref && (
              <Button asChild variant="outline">
                <a href={exportHref} download>
                  <DownloadIcon aria-hidden="true" />
                  {strings.reconciliation.exportCsv}
                </a>
              </Button>
            )}
            {canWrite && <RerunButton run={run} />}
          </div>
        )}
      </div>
      <dl className="grid grid-cols-2 gap-x-6 gap-y-3 rounded-lg border bg-card p-4 md:grid-cols-4">
        <Fact label={strings.reconciliation.connection}>
          <Link
            to={`/connections/${run.connection.id}`}
            className="inline-flex max-w-full rounded-sm underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <RunConnection run={run} />
          </Link>
        </Fact>
        <Fact label={strings.reconciliation.range}>{formatRunRange(run.from, run.to)}</Fact>
        <Fact label={strings.reconciliation.runCount}>
          <span className="tabular-nums">{run.run_count}</span>
        </Fact>
        <Fact label={strings.reconciliation.startedBy}>{run.created_by?.name ?? strings.reconciliation.system}</Fact>
        <Fact label={strings.reconciliation.created}>
          <DateTimeText value={run.created_at} />
        </Fact>
        <Fact label={strings.reconciliation.started}>
          <DateTimeText value={run.started_at} />
        </Fact>
        <Fact label={strings.reconciliation.finished}>
          <DateTimeText value={run.finished_at} />
        </Fact>
      </dl>
    </div>
  );
}

export function RunHeaderSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true" data-testid="run-header-skeleton">
      <Skeleton className="h-4 w-40" />
      <Skeleton className="h-7 w-80 max-w-full" />
      <Skeleton className="h-24 w-full" />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} className="h-16" />
        ))}
      </div>
    </div>
  );
}
