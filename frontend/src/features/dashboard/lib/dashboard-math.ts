import { formatPercent } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import type { StatDelta } from "@/shared/ui/stat-card";
import type { DashboardCurrencyTotals, DashboardPaymentTotals, DashboardSeriesPoint } from "../api/dashboard";

function direction(diff: number): StatDelta["direction"] {
  if (diff > 0) return "up";
  if (diff < 0) return "down";
  return "flat";
}

function tone(diff: number): StatDelta["tone"] {
  if (diff > 0) return "positive";
  if (diff < 0) return "negative";
  return "neutral";
}

/** The change of a count or amount versus the previous period, as text with a direction. */
export function deltaOf(current: number, previous: number, days: number): StatDelta {
  if (previous === 0) {
    if (current === 0) return { direction: "flat", text: strings.dashboard.noChange, tone: "neutral" };
    return { direction: "up", text: strings.dashboard.newInRange, tone: "positive" };
  }
  const ratio = (current - previous) / previous;
  return { direction: direction(ratio), text: `${formatPercent(ratio, { signed: true })} ${strings.dashboard.vsPrevious(days)}`, tone: tone(ratio) };
}

const pointsFormat = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1, signDisplay: "exceptZero" });

/** The change of a rate in percentage points; nothing when either period has no rate. */
export function ratePoints(current: number | null, previous: number | null, days: number): StatDelta | undefined {
  if (current === null || previous === null) return undefined;
  const points = (current - previous) * 100;
  if (Math.abs(points) < 0.05) return { direction: "flat", text: strings.dashboard.noChange, tone: "neutral" };
  return {
    direction: direction(points),
    text: `${pointsFormat.format(points)} ${strings.dashboard.pointsSuffix} ${strings.dashboard.vsPrevious(days)}`,
    tone: tone(points),
  };
}

/** Currency codes with IDR first, then alphabetical, without duplicates. */
export function sortCurrencies(codes: Iterable<string>): string[] {
  return [...new Set(codes)].sort((a, b) => (a === "IDR" ? -1 : b === "IDR" ? 1 : a.localeCompare(b)));
}

/** The daily values of a count, or of the gross amount in one currency. */
export function sparklineFrom(series: DashboardSeriesPoint[], key: "created" | "successful" | { gross: string }): number[] {
  if (typeof key === "string") return series.map((point) => point[key]);
  return series.map((point) => point.gross.find((g) => g.currency === key.gross)?.amount ?? 0);
}

/** Status counts added up across every currency; counts are currency-free, only `gross` is not. */
export function sumTotals(byCurrency: DashboardCurrencyTotals[], period: "current" | "previous"): DashboardPaymentTotals {
  const sum = { created: 0, pending: 0, paid: 0, settled: 0, expired: 0, failed: 0, successful: 0 };
  for (const entry of byCurrency) {
    const totals = entry[period];
    for (const key of Object.keys(sum) as (keyof typeof sum)[]) sum[key] += totals[key];
  }
  const finished = sum.successful + sum.expired + sum.failed;
  return { ...sum, gross: 0, success_rate: finished === 0 ? null : sum.successful / finished };
}
