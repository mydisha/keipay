import { formatPercent } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import type { StatChange } from "@/shared/ui/stat-strip";
import type { DashboardCurrencyTotals, DashboardPaymentTotals } from "../api/dashboard";

function tone(diff: number): StatChange["tone"] {
  return diff > 0 ? "positive" : "negative";
}

/**
 * The change of a count or amount against the previous period, as a signed percentage. The page
 * header names the period once, so the text stays short. Nothing when there is no real change:
 * an empty previous period has nothing to compare with.
 */
export function deltaOf(current: number, previous: number): StatChange | undefined {
  if (previous === 0 || current === previous) return undefined;
  const ratio = (current - previous) / previous;
  return { text: formatPercent(ratio, { signed: true }), tone: tone(ratio) };
}

const pointsFormat = new Intl.NumberFormat("id-ID", { maximumFractionDigits: 1, signDisplay: "exceptZero" });

/** The change of a rate in percentage points; nothing when either period has no rate or it did not move. */
export function ratePoints(current: number | null, previous: number | null): StatChange | undefined {
  if (current === null || previous === null) return undefined;
  const points = (current - previous) * 100;
  if (Math.abs(points) < 0.05) return undefined;
  return { text: `${pointsFormat.format(points)} ${strings.dashboard.pointsSuffix}`, tone: tone(points) };
}

/** Currency codes with IDR first, then alphabetical, without duplicates. */
export function sortCurrencies(codes: Iterable<string>): string[] {
  return [...new Set(codes)].sort((a, b) => (a === "IDR" ? -1 : b === "IDR" ? 1 : a.localeCompare(b)));
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
