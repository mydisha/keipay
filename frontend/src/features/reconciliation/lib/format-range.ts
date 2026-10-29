import { formatDateWIB, formatMonthWIB } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { inclusiveEndDate, wibDate, wibDateInstant } from "@/shared/lib/wib-dates";
import type { ReportInterval, SummaryRow } from "../api/reports";

/** A half-open range of instants as the WIB days it covers: "1 Okt 2026 – 7 Okt 2026". */
export function formatRunRange(from: string, to: string): string {
  const first = wibDate(new Date(from));
  const last = inclusiveEndDate(to);
  const firstText = formatDateWIB(wibDateInstant(first));
  if (last <= first) return firstText;
  return `${firstText} – ${formatDateWIB(wibDateInstant(last))}`;
}

/** A summary row's period: "Total", a WIB day, or a WIB month. */
export function formatPeriod(row: Pick<SummaryRow, "period_start">, interval: ReportInterval): string {
  if (interval === "total") return strings.reconciliation.periodTotal;
  const start = new Date(row.period_start);
  return interval === "month" ? formatMonthWIB(start) : formatDateWIB(start);
}
