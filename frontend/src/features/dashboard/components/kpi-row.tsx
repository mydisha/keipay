import { formatMoney, formatNumber, formatPercent } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { Stat, StatStrip } from "@/shared/ui/stat-strip";
import type { Dashboard } from "../api/dashboard";
import { deltaOf, ratePoints, sumTotals } from "../lib/dashboard-math";

interface KpiRowProps {
  dashboard: Dashboard;
  /** The currency the gross figure is shown in; `null` when the range has no payments. */
  currency: string | null;
  className?: string;
}

/**
 * Gross volume, payments, success rate and delivery success. Only gross follows the chosen
 * currency; counts and rates cover every currency because they never add amounts.
 */
export function KpiRow({ dashboard, currency, className }: KpiRowProps) {
  const { by_currency: byCurrency } = dashboard.payments;
  const gross = currency ? byCurrency.find((entry) => entry.currency === currency) : undefined;
  const current = sumTotals(byCurrency, "current");
  const previous = sumTotals(byCurrency, "previous");
  const finished = current.successful + current.expired + current.failed;
  const deliveries = dashboard.deliveries;
  const deliveriesFinished = deliveries.delivered + deliveries.failed;

  return (
    <StatStrip aria-label={strings.dashboard.figuresLabel} className={className}>
      <Stat
        label={strings.dashboard.grossVolume}
        value={gross ? formatMoney(gross.current.gross, gross.currency) : strings.common.none}
        empty={!gross}
        change={gross ? deltaOf(gross.current.gross, gross.previous.gross) : undefined}
      />
      <Stat label={strings.dashboard.paymentsCount} value={formatNumber(current.created)} change={deltaOf(current.created, previous.created)} />
      <Stat
        label={strings.dashboard.successRate}
        value={current.success_rate === null ? strings.common.notYet : formatPercent(current.success_rate)}
        empty={current.success_rate === null}
        change={ratePoints(current.success_rate, previous.success_rate)}
        hint={finished === 0 ? strings.dashboard.noFinishedPayments : strings.dashboard.successfulOf(current.successful, finished)}
      />
      <Stat
        label={strings.dashboard.deliverySuccess}
        value={deliveries.success_rate === null ? strings.common.notYet : formatPercent(deliveries.success_rate)}
        empty={deliveries.success_rate === null}
        hint={deliveriesFinished === 0 ? strings.dashboard.noFinishedDeliveries : strings.dashboard.deliveredOf(deliveries.delivered, deliveriesFinished)}
      />
    </StatStrip>
  );
}
