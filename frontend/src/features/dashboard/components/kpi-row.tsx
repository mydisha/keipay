import { formatMoney, formatNumber, formatPercent } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { Sparkline } from "@/shared/ui/sparkline";
import { StatCard } from "@/shared/ui/stat-card";
import { rangeDays, type Dashboard } from "../api/dashboard";
import { deltaOf, ratePoints, sparklineFrom, sumTotals } from "../lib/dashboard-math";

interface KpiRowProps {
  dashboard: Dashboard;
  /** The currency the gross figure is shown in; `null` when the range has no payments. */
  currency: string | null;
}

const NONE = strings.common.none;

/** Gross volume, payment count, success rate and delivery success, each against the previous period. */
export function KpiRow({ dashboard, currency }: KpiRowProps) {
  const days = rangeDays(dashboard.range);
  const { by_currency: byCurrency, series } = dashboard.payments;
  const gross = currency ? byCurrency.find((entry) => entry.currency === currency) : undefined;
  const current = sumTotals(byCurrency, "current");
  const previous = sumTotals(byCurrency, "previous");
  const finished = current.successful + current.expired + current.failed;
  const deliveriesFinished = dashboard.deliveries.delivered + dashboard.deliveries.failed;

  return (
    <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
      <StatCard
        label={strings.dashboard.grossVolume}
        value={gross ? formatMoney(gross.current.gross, gross.currency) : NONE}
        delta={gross ? deltaOf(gross.current.gross, gross.previous.gross, days) : undefined}
        hint={gross ? gross.currency : strings.dashboard.noPayments}
        sparkline={gross && <Sparkline values={sparklineFrom(series, { gross: gross.currency })} />}
      />
      <StatCard
        label={strings.dashboard.paymentsCount}
        value={formatNumber(current.created)}
        delta={deltaOf(current.created, previous.created, days)}
        sparkline={<Sparkline values={sparklineFrom(series, "created")} />}
      />
      <StatCard
        label={strings.dashboard.successRate}
        value={current.success_rate === null ? NONE : formatPercent(current.success_rate)}
        delta={ratePoints(current.success_rate, previous.success_rate, days)}
        hint={finished === 0 ? strings.dashboard.noFinishedPayments : strings.dashboard.successfulOf(current.successful, finished)}
        sparkline={<Sparkline values={sparklineFrom(series, "successful")} />}
      />
      <StatCard
        label={strings.dashboard.deliverySuccess}
        value={dashboard.deliveries.success_rate === null ? NONE : formatPercent(dashboard.deliveries.success_rate)}
        hint={
          deliveriesFinished === 0
            ? strings.dashboard.noFinishedDeliveries
            : strings.dashboard.deliveredOf(dashboard.deliveries.delivered, deliveriesFinished)
        }
      />
    </div>
  );
}
