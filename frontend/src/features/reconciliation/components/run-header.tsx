import { DownloadIcon, RotateCwIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card, CardContent } from "@/shared/ui/card";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { CopyButton } from "@/shared/ui/copy-button";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { DetailHeader, DetailHeaderSkeleton } from "@/shared/ui/detail-header";
import { Fact, FactList } from "@/shared/ui/fact-list";
import { useRerun, type ReconciliationRun } from "../api/runs";
import { formatRunRange } from "../lib/format-range";
import { RUNS_TAB_PATH } from "../lib/reconciliation-search";
import { RunStatusBadge } from "./badges";
import { RunConnection } from "./run-connection";

export const backToRuns = { to: RUNS_TAB_PATH, label: strings.reconciliation.backToRuns };

/** The toast for a failed re-run: `409 run_in_progress` says why. */
function rerunErrorMessage(error: unknown): string {
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

/** Back link, the run id as the title, its status, key facts and the actions. */
export function RunHeader({ run, exportHref }: { run: ReconciliationRun; exportHref: string | null }) {
  const canWrite = usePermission("reconciliation:write");
  return (
    <DetailHeader
      back={backToRuns}
      eyebrow={strings.reconciliation.runTitle}
      title={run.id}
      titleMono
      badges={
        <>
          <CopyButton value={run.id} label={`${strings.common.copyValue(strings.reconciliation.runId)} ${run.id}`} />
          <RunStatusBadge status={run.status} />
        </>
      }
      actions={
        (exportHref || canWrite) && (
          <>
            {exportHref && (
              <Button asChild variant="outline">
                <a href={exportHref} download>
                  <DownloadIcon aria-hidden="true" />
                  {strings.reconciliation.exportCsv}
                </a>
              </Button>
            )}
            {canWrite && <RerunButton run={run} />}
          </>
        )
      }
      facts={
        <Card size="compact">
          <CardContent>
            <FactList layout="tiles">
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
                <DateTimeText value={run.started_at} fallback={strings.common.notYet} />
              </Fact>
              <Fact label={strings.reconciliation.finished}>
                <DateTimeText value={run.finished_at} fallback={strings.common.notYet} />
              </Fact>
            </FactList>
          </CardContent>
        </Card>
      }
    />
  );
}

export function RunHeaderSkeleton() {
  return <DetailHeaderSkeleton testId="run-header-skeleton" />;
}
