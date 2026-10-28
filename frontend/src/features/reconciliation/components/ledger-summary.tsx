import { AppLabel } from "@/features/apps/components/app-label";
import { formatNumber } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { Card } from "@/shared/ui/card";
import { MoneyText } from "@/shared/ui/money-text";
import { Skeleton } from "@/shared/ui/skeleton";
import type { LedgerSummary as Summary } from "../api/ledger";

export function LedgerSummary({
  summary,
  isLoading,
}: {
  summary: Summary[] | undefined;
  isLoading: boolean;
}) {
  if (isLoading) {
    return (
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-busy="true">
        {Array.from({ length: 3 }, (_, i) => (
          <Skeleton key={i} data-testid="skeleton-summary" className="h-32" />
        ))}
      </div>
    );
  }
  if (!summary || summary.length === 0) return null;
  return (
    <ul className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3" aria-label={strings.reconciliation.summaryTitle}>
      {summary.map((row) => (
        <li key={row.app.id}>
          <Card className="grid gap-3 p-4">
            <div className="flex items-baseline justify-between gap-2">
              <span className="min-w-0 font-medium">
                <AppLabel app={row.app} />
              </span>
              <span className="text-xs text-muted-foreground tabular-nums">
                {formatNumber(row.count)} {strings.reconciliation.count.toLowerCase()}
              </span>
            </div>
            <dl className="grid grid-cols-3 gap-2 text-sm">
              <SummaryFigure label={strings.reconciliation.gross} amount={row.gross_amount} />
              <SummaryFigure label={strings.reconciliation.fee} amount={row.total_fee} />
              <SummaryFigure label={strings.reconciliation.net} amount={row.net_amount} strong />
            </dl>
          </Card>
        </li>
      ))}
    </ul>
  );
}

function SummaryFigure({ label, amount, strong = false }: { label: string; amount: number; strong?: boolean }) {
  return (
    <div className="grid min-w-0 gap-0.5">
      <dt className="text-right text-xs text-muted-foreground">{label}</dt>
      <dd className={strong ? "font-semibold" : undefined}>
        <MoneyText amount={amount} className="truncate" />
      </dd>
    </div>
  );
}
