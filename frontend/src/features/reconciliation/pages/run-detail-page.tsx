import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { Link, useParams } from "react-router";
import { RequirePermission, usePermission } from "@/features/auth/components/can";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription, AlertTitle } from "@/shared/ui/alert";
import { BackLink } from "@/shared/ui/back-link";
import { Button } from "@/shared/ui/button";
import { Card, CardContent } from "@/shared/ui/card";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { PageHeader } from "@/shared/ui/page-header";
import { itemsExportUrl, useReconciliationRun, type ReconciliationRun } from "../api/runs";
import { ItemSheet } from "../components/item-sheet";
import { backToRuns, RunHeader, RunHeaderSkeleton } from "../components/run-header";
import { RunResults } from "../components/run-results";
import { isNotFound } from "../lib/is-not-found";
import { RUNS_TAB_PATH, useRunSearch } from "../lib/reconciliation-search";

/** `/reconciliation/runs/:id`: a run's facts, its progress or failure, and its items. */
export function RunDetailPage() {
  const { id = "" } = useParams();
  return (
    <RequirePermission permission="payments:read">
      <RunDetail id={id} />
    </RequirePermission>
  );
}

function RunFailed({ run }: { run: ReconciliationRun }) {
  const canWrite = usePermission("reconciliation:write");
  return (
    <Alert variant="danger" data-testid="run-failed">
      <CircleAlertIcon aria-hidden="true" />
      <AlertTitle>{strings.reconciliation.failedTitle}</AlertTitle>
      <AlertDescription className="grid gap-1">
        {run.error && <p>{run.error.message}</p>}
        {run.error && <p className="font-mono text-xs">{strings.reconciliation.failedCode(run.error.code)}</p>}
        {canWrite && <p>{strings.reconciliation.failedRerunHint}</p>}
      </AlertDescription>
    </Alert>
  );
}

function RunInProgress({ run }: { run: ReconciliationRun }) {
  return (
    <Card size="compact" role="status" data-testid="run-progress">
      <CardContent className="flex items-start gap-3">
        <Loader2Icon className="mt-0.5 size-5 shrink-0 animate-spin text-muted-foreground motion-reduce:animate-none" aria-hidden="true" />
        <div className="grid gap-1">
          <p className="font-medium">{strings.reconciliation.progressTitle}</p>
          <p className="text-sm text-muted-foreground">
            {run.status === "queued" ? strings.reconciliation.progressQueued : strings.reconciliation.progressRunning}
          </p>
        </div>
      </CardContent>
    </Card>
  );
}

function RunDetail({ id }: { id: string }) {
  const run = useReconciliationRun(id);
  const search = useRunSearch();

  if (run.isPending) {
    return (
      <>
        <RunHeaderSkeleton />
        <span className="sr-only" aria-live="polite">
          {strings.common.loading}
        </span>
      </>
    );
  }

  if (run.isError) {
    return (
      <>
        <BackLink {...backToRuns} />
        <PageHeader title={strings.reconciliation.runTitle} />
        {isNotFound(run.error) ? (
          <EmptyState
            title={strings.reconciliation.notFoundTitle}
            description={strings.reconciliation.notFoundBody}
            action={
              <Button asChild variant="outline">
                <Link to={RUNS_TAB_PATH}>{strings.reconciliation.backToRuns}</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState error={run.error} onRetry={() => void run.refetch()} />
        )}
      </>
    );
  }

  const current = run.data;
  const succeeded = current.status === "succeeded";
  return (
    <>
      <RunHeader run={current} exportHref={succeeded ? itemsExportUrl(current.id, search.filters) : null} />
      {current.status === "failed" && <RunFailed run={current} />}
      {(current.status === "queued" || current.status === "running") && <RunInProgress run={current} />}
      {succeeded && <RunResults run={current} search={search} />}
      {succeeded && <ItemSheet run={current} itemId={search.itemId} onClose={search.closeItem} />}
    </>
  );
}
