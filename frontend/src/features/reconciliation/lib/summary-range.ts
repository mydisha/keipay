import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { daysInRange, isDateString } from "@/shared/lib/wib-dates";
import type { ReportInterval } from "../api/reports";

// Mirrors the server's `max_range` rule for GET /admin/payment-reports/summary.
export const MAX_SUMMARY_DAYS = 366;
export const MAX_DAILY_SUMMARY_DAYS = 92;

/** Why a picked range can't be summarised (shown instead of sending the request), or null. */
export function summaryRangeError(range: { from: string | null; to: string | null }, interval: ReportInterval): string | null {
  const { from, to } = range;
  if (!isDateString(from)) return strings.reconciliation.rangeFromRequired;
  if (!isDateString(to)) return strings.reconciliation.rangeToRequired;
  const days = daysInRange(from, to);
  if (days < 1) return strings.reconciliation.rangeOrder;
  if (interval === "day" && days > MAX_DAILY_SUMMARY_DAYS) return strings.reconciliation.rangeTooLongDay;
  if (days > MAX_SUMMARY_DAYS) return strings.reconciliation.rangeTooLong(MAX_SUMMARY_DAYS);
  return null;
}

/** A `400 invalid_request` on `from`/`to` from the server, as the message under the date range. */
export function serverRangeError(error: unknown, interval: ReportInterval): string | null {
  if (!isApiError(error) || error.code !== "invalid_request") return null;
  const field = error.fields.find((f) => f.field === "from" || f.field === "to" || f.field === "interval");
  if (!field) return null;
  if (field.rule === "max_range") {
    return interval === "day" ? strings.reconciliation.rangeTooLongDay : strings.reconciliation.rangeTooLong(MAX_SUMMARY_DAYS);
  }
  if (field.rule === "after") return strings.reconciliation.rangeOrder;
  return strings.reconciliation.rangeInvalid;
}
