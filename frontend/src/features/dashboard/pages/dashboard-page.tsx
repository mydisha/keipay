import { useState } from "react";
import { RequirePermission } from "@/features/auth/components/can";
import { useMode, type Mode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { ErrorState } from "@/shared/ui/error-state";
import { PageHeader } from "@/shared/ui/page-header";
import { useDashboard, type DashboardRange } from "../api/dashboard";
import { AttentionCard } from "../components/attention-card";
import { ConnectionsHealthCard } from "../components/connections-health-card";
import { DashboardSkeleton } from "../components/dashboard-skeleton";
import { KpiRow } from "../components/kpi-row";
import { RangeChips } from "../components/range-chips";
import { RecentPayments } from "../components/recent-payments";
import { VolumeChart } from "../components/volume-chart";
import { sortCurrencies } from "../lib/dashboard-math";
import { useDashboardRange } from "../lib/dashboard-search";

/** `/`: the period from `?range=`, the environment from the Test/Live switch. */
export function DashboardPage() {
  const { mode } = useMode();
  const [range, setRange] = useDashboardRange();
  return (
    <>
      <PageHeader title={strings.dashboard.title} description={strings.dashboard.subtitle} actions={<RangeChips value={range} onChange={setRange} />}>
        <p className="text-sm text-muted-foreground">{strings.dashboard.environmentNote(strings.environments[mode] ?? mode)}</p>
      </PageHeader>
      <RequirePermission permission="payments:read">
        <DashboardContent range={range} environment={mode} />
      </RequirePermission>
    </>
  );
}

function DashboardContent({ range, environment }: { range: DashboardRange; environment: Mode }) {
  const query = useDashboard({ range, environment });
  // The chosen currency survives a reload of the data; it falls back to the first one the range has.
  const [chosen, setChosen] = useState<string | null>(null);

  if (query.isPending) return <DashboardSkeleton />;
  if (query.isError) return <ErrorState error={query.error} onRetry={() => void query.refetch()} />;

  const dashboard = query.data;
  const currencies = sortCurrencies(dashboard.payments.by_currency.map((entry) => entry.currency));
  const currency = chosen && currencies.includes(chosen) ? chosen : (currencies[0] ?? null);

  return (
    <>
      <KpiRow dashboard={dashboard} currency={currency} />
      <div className="grid gap-4 xl:grid-cols-[2fr_1fr] xl:items-start">
        <VolumeChart dashboard={dashboard} currencies={currencies} currency={currency} onCurrencyChange={setChosen} />
        <div className="grid gap-4">
          <ConnectionsHealthCard connections={dashboard.connections} />
          <AttentionCard dashboard={dashboard} />
        </div>
      </div>
      <RecentPayments payments={dashboard.recent_payments} />
    </>
  );
}
