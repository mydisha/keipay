import { ChevronRightIcon, CircleCheckBigIcon, type LucideIcon, ScaleIcon, ShieldAlertIcon, WebhookIcon } from "lucide-react";
import { Link } from "react-router";
import { RunStatusBadge } from "@/features/reconciliation/components/badges";
import { runPath, RUNS_TAB_PATH } from "@/features/reconciliation/lib/reconciliation-search";
import { strings } from "@/shared/lib/strings";
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/shared/ui/card";
import { DateTimeText } from "@/shared/ui/date-time-text";
import type { Dashboard } from "../api/dashboard";

export const FAILED_DELIVERIES_PATH = "/webhooks?status=FAILED";
export const INBOUND_PROBLEMS_PATH = "/webhooks?tab=inbound";

interface Problem {
  id: string;
  icon: LucideIcon;
  title: string;
  hint: string;
  to: string;
}

function problemsOf(dashboard: Dashboard): Problem[] {
  const problems: Problem[] = [];
  if (dashboard.deliveries.failed > 0) {
    problems.push({
      id: "deliveries",
      icon: WebhookIcon,
      title: strings.dashboard.failedDeliveries(dashboard.deliveries.failed),
      hint: strings.dashboard.failedDeliveriesHint,
      to: FAILED_DELIVERIES_PATH,
    });
  }
  if (dashboard.inbound.problems > 0) {
    problems.push({
      id: "inbound",
      icon: ShieldAlertIcon,
      title: strings.dashboard.inboundProblems(dashboard.inbound.problems),
      hint: strings.dashboard.inboundProblemsHint,
      to: INBOUND_PROBLEMS_PATH,
    });
  }
  if (dashboard.reconciliation.unresolved_items > 0) {
    problems.push({
      id: "unresolved",
      icon: ScaleIcon,
      title: strings.dashboard.unresolvedItems(dashboard.reconciliation.unresolved_items),
      hint: strings.dashboard.unresolvedItemsHint,
      to: RUNS_TAB_PATH,
    });
  }
  return problems;
}

const linkClass =
  "flex items-center gap-3 rounded-md px-(--card-pad) py-3 outline-none transition-colors hover:bg-muted/40 focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11";

/** Failed deliveries, rejected callbacks and unresolved reconciliation items, each linking to where to fix them. */
export function AttentionCard({ dashboard }: { dashboard: Dashboard }) {
  const problems = problemsOf(dashboard);
  const run = dashboard.reconciliation.latest_run;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{strings.dashboard.attentionTitle}</CardTitle>
        <CardDescription>{strings.dashboard.attentionBody}</CardDescription>
      </CardHeader>
      <CardContent className="p-0">
        {problems.length === 0 ? (
          <div className="flex items-start gap-3 px-(--card-pad) py-4" data-testid="all-clear">
            <CircleCheckBigIcon className="mt-0.5 size-5 shrink-0 text-success-foreground" aria-hidden="true" />
            <div className="grid gap-0.5">
              <p className="font-medium">{strings.dashboard.allClearTitle}</p>
              <p className="text-sm text-muted-foreground">{strings.dashboard.allClearBody}</p>
            </div>
          </div>
        ) : (
          <ul aria-label={strings.dashboard.attentionTitle} className="divide-y">
            {problems.map((problem) => (
              <li key={problem.id}>
                <Link to={problem.to} className={linkClass}>
                  <problem.icon className="size-4 shrink-0 text-danger-foreground" aria-hidden="true" />
                  <span className="grid min-w-0 flex-1 gap-0.5">
                    <span className="font-medium">{problem.title}</span>
                    <span className="text-xs text-muted-foreground">{problem.hint}</span>
                  </span>
                  <ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" aria-hidden="true" />
                </Link>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
      <CardFooter className="flex-col items-stretch gap-1 text-sm">
        <span className="text-xs text-muted-foreground">{strings.dashboard.latestRun}</span>
        {run ? (
          <Link
            to={runPath(run.id)}
            className="flex min-w-0 flex-wrap items-center gap-2 rounded-sm outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50"
          >
            <span className="truncate font-mono text-[0.8125rem] underline-offset-4 hover:underline" title={run.id}>
              {run.id}
            </span>
            <RunStatusBadge status={run.status} />
            <DateTimeText value={run.created_at} className="text-muted-foreground" />
          </Link>
        ) : (
          <span className="text-muted-foreground">{strings.dashboard.noRuns}</span>
        )}
      </CardFooter>
    </Card>
  );
}
