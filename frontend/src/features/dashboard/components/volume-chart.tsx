import type { CSSProperties } from "react";
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts";
import type { TooltipPayloadEntry } from "recharts";
import { formatDayWIB, formatMoney, formatNumber, parseDate } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, type ChartConfig } from "@/shared/ui/chart";
import { Chip } from "@/shared/ui/chip";
import { EmptyState } from "@/shared/ui/empty-state";
import { rangeDays, type Dashboard, type DashboardSeriesPoint } from "../api/dashboard";

// Slate for every payment created, pine for the ones that succeeded: the signal marks the outcome that matters.
const CREATED = "var(--chart-3)";
const SUCCESSFUL = "var(--chart-1)";

const chartConfig: ChartConfig = {
  created: { label: strings.dashboard.created, color: CREATED },
  successful: { label: strings.dashboard.successful, color: SUCCESSFUL },
};

interface DayRow {
  day: string;
  created: number;
  successful: number;
  gross: DashboardSeriesPoint["gross"];
}

function dayLabel(point: DashboardSeriesPoint): string {
  const date = parseDate(point.period_start);
  return date ? formatDayWIB(date) : point.period_start;
}

function isDayRow(value: unknown): value is DayRow {
  return typeof value === "object" && value !== null && "gross" in value && Array.isArray((value as DayRow).gross);
}

/** Counts of the day plus the gross amount per currency. */
function VolumeTooltip({ active, payload, label }: { active?: boolean; payload?: ReadonlyArray<TooltipPayloadEntry>; label?: unknown }) {
  const first = payload?.[0];
  if (!active || !first || !isDayRow(first.payload)) return null;
  const row = first.payload;
  return (
    <div className="grid min-w-40 gap-1.5 rounded-md border bg-popover px-2.5 py-1.5 text-xs text-popover-foreground shadow-md">
      <div className="font-medium">{typeof label === "string" ? label : row.day}</div>
      <div className="grid gap-1">
        <Row label={strings.dashboard.created} value={formatNumber(row.created)} color={CREATED} />
        <Row label={strings.dashboard.successful} value={formatNumber(row.successful)} color={SUCCESSFUL} />
        {row.gross.map((entry) => (
          <Row key={entry.currency} label={`${strings.dashboard.gross} ${entry.currency}`} value={formatMoney(entry.amount, entry.currency)} />
        ))}
      </div>
    </div>
  );
}

function Row({ label, value, color }: { label: string; value: string; color?: string }) {
  return (
    <div className="flex items-center gap-2">
      {color && <span aria-hidden="true" className="size-2.5 shrink-0 rounded-[2px] bg-(--indicator-color)" style={{ "--indicator-color": color } as CSSProperties} />}
      <span className="text-muted-foreground">{label}</span>
      <span className="ml-auto font-medium tabular-nums">{value}</span>
    </div>
  );
}

interface VolumeChartProps {
  dashboard: Dashboard;
  /** Every currency of the range, IDR first. */
  currencies: string[];
  currency: string | null;
  onCurrencyChange: (currency: string) => void;
}

/**
 * Created versus successful payments per day, with the gross per currency in the tooltip and a text
 * table for screen readers. It sits under the figures in the same surface, so it has no card of its own.
 */
export function VolumeChart({ dashboard, currencies, currency, onCurrencyChange }: VolumeChartProps) {
  const days = rangeDays(dashboard.range);
  const series = dashboard.payments.series;
  const rows: DayRow[] = series.map((point) => ({ day: dayLabel(point), created: point.created, successful: point.successful, gross: point.gross }));
  const empty = rows.every((row) => row.created === 0);

  return (
    <section aria-labelledby="volume-heading" className="grid grid-cols-1 gap-4 p-4 md:p-5">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 id="volume-heading" className="text-sm font-medium">
          {strings.dashboard.volumeTitle}
        </h2>
        {currencies.length > 1 && (
          <div role="group" aria-label={strings.dashboard.currencyFilter} className="flex flex-wrap gap-1.5">
            {currencies.map((code) => (
              <Chip key={code} pressed={code === currency} onClick={() => onCurrencyChange(code)}>
                {code}
              </Chip>
            ))}
          </div>
        )}
      </div>
      <div className="min-w-0">
        {empty ? (
          <EmptyState
            variant="inline"
            headingLevel={3}
            title={strings.dashboard.volumeEmptyTitle}
            description={strings.dashboard.volumeEmptyBody}
            className="py-6"
          />
        ) : (
          <>
            <ChartContainer config={chartConfig} label={strings.dashboard.chartLabel(days)} className="h-64 w-full">
              <BarChart data={rows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }} barCategoryGap="24%">
                <CartesianGrid vertical={false} />
                <XAxis dataKey="day" tickLine={false} axisLine={false} tickMargin={8} minTickGap={16} />
                <YAxis allowDecimals={false} tickLine={false} axisLine={false} width={32} />
                <ChartTooltip cursor={{ fillOpacity: 0.4 }} content={<VolumeTooltip />} />
                <ChartLegend content={<ChartLegendContent />} />
                <Bar dataKey="created" fill="var(--color-created)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
                <Bar dataKey="successful" fill="var(--color-successful)" radius={[2, 2, 0, 0]} maxBarSize={24} isAnimationActive={false} />
              </BarChart>
            </ChartContainer>
            <table className="sr-only">
              <caption>{strings.dashboard.volumeTitle}</caption>
              <thead>
                <tr>
                  <th scope="col">{strings.dashboard.day}</th>
                  <th scope="col">{strings.dashboard.created}</th>
                  <th scope="col">{strings.dashboard.successful}</th>
                  {currencies.map((code) => (
                    <th key={code} scope="col">
                      {strings.dashboard.gross} {code}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.day}>
                    <th scope="row">{row.day}</th>
                    <td>{formatNumber(row.created)}</td>
                    <td>{formatNumber(row.successful)}</td>
                    {currencies.map((code) => (
                      <td key={code}>{formatMoney(row.gross.find((g) => g.currency === code)?.amount ?? 0, code)}</td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </>
        )}
      </div>
    </section>
  );
}
