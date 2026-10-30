import { Link } from "react-router";
import { RunStatusBadge } from "@/features/reconciliation/components/badges";
import { runPath, RUNS_TAB_PATH } from "@/features/reconciliation/lib/reconciliation-search";
import { strings } from "@/shared/lib/strings";
import { Card, CardHeader, CardTitle } from "@/shared/ui/card";
import { DateTimeText } from "@/shared/ui/date-time-text";
import type { Dashboard } from "../api/dashboard";

export const FAILED_DELIVERIES_PATH = "/webhooks?status=FAILED";
export const INBOUND_PROBLEMS_PATH = "/webhooks?tab=inbound";

interface Problem {
  id: string;
  title: string;
  hint: string;
  to: string;
}

function problemsOf(dashboard: Dashboard): Problem[] {
  const problems: Problem[] = [];
  if (dashboard.deliveries.failed > 0) {
    problems.push({
      id: "deliveries",
      title: strings.dashboard.failedDeliveries(dashboard.deliveries.failed),
      hint: strings.dashboard.failedDeliveriesHint,
      to: FAILED_DELIVERIES_PATH,
    });
  }
  if (dashboard.inbound.problems > 0) {
    problems.push({
      id: "inbound",
      title: strings.dashboard.inboundProblems(dashboard.inbound.problems),
      hint: strings.dashboard.inboundProblemsHint,
      to: INBOUND_PROBLEMS_PATH,
    });
  }
  if (dashboard.reconciliation.unresolved_items > 0) {
    problems.push({
      id: "unresolved",
      title: strings.dashboard.unresolvedItems(dashboard.reconciliation.unresolved_items),
      hint: strings.dashboard.unresolvedItemsHint,
      to: RUNS_TAB_PATH,
    });
  }
  return problems;
}

const rowClass = "px-(--card-pad) py-3";
const linkClass =
  "grid gap-0.5 px-(--card-pad) py-3 outline-none transition-colors hover:bg-muted/60 focus-visible:ring-[3px] focus-visible:ring-ring/50 focus-visible:ring-inset max-md:min-h-11";

/**
 * What someone has to fix, as ruled lines: failed deliveries, rejected callbacks and unresolved
 * reconciliation items, each a link to the page that fixes it, then the latest reconciliation run.
 */
export function AttentionCard({ dashboard }: { dashboard: Dashboard }) {
  const problems = problemsOf(dashboard);
  const run = dashboard.reconciliation.latest_run;
  return (
    <Card className="overflow-hidden">
      <CardHeader>
        <CardTitle>{strings.dashboard.attentionTitle}</CardTitle>
      </CardHeader>
      <ul aria-label={strings.dashboard.attentionTitle} className="divide-y">
        {problems.length === 0 ? (
          <li className={`${rowClass} text-sm text-muted-foreground`} data-testid="all-clear">
            {strings.dashboard.allClear}
          </li>
        ) : (
          problems.map((problem) => (
            <li key={problem.id}>
              <Link to={problem.to} className={linkClass}>
                <span className="font-medium text-danger-foreground underline-offset-4 hover:underline">{problem.title}</span>
                <span className="text-xs text-muted-foreground">{problem.hint}</span>
              </Link>
            </li>
          ))
        )}
        <li className={`${rowClass} grid gap-1 text-sm`}>
          <span className="text-xs text-muted-foreground">{strings.dashboard.latestRun}</span>
          {run ? (
            <span className="flex min-w-0 flex-wrap items-center gap-2">
              <Link
                to={runPath(run.id)}
                title={run.id}
                className="truncate rounded-sm font-mono text-[0.8125rem] text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
              >
                {run.id}
              </Link>
              <RunStatusBadge status={run.status} />
              <DateTimeText value={run.created_at} className="text-muted-foreground" />
            </span>
          ) : (
            <span className="text-muted-foreground">{strings.dashboard.noRuns}</span>
          )}
        </li>
      </ul>
    </Card>
  );
}
