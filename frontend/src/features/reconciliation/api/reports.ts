import { skipToken, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";
import { withQuery } from "@/shared/lib/url";

export type PaymentReport = components["schemas"]["PaymentReport"];
export type SummaryRow = components["schemas"]["PaymentReportSummaryRow"];
export type ReportInterval = components["schemas"]["ReportInterval"];
type Environment = components["schemas"]["AppEnvironment"];

export const REPORT_INTERVALS: readonly ReportInterval[] = ["total", "day", "month"];
export const REPORTS_PAGE_SIZE = 50;

export function isReportInterval(value: string | null): value is ReportInterval {
  return value !== null && (REPORT_INTERVALS as readonly string[]).includes(value);
}

/** Filters shared by the summary, the report list and the CSV export; `from`/`to` are instants. */
export interface ReportFilters {
  from: string;
  to: string;
  environment: Environment;
  /** App public id (`app_…`). */
  appId: string | null;
  connectionId: string | null;
  currency: string | null;
}

/** The query string the list and the export share (no paging). */
export function reportQuery(filters: ReportFilters) {
  return {
    from: filters.from,
    to: filters.to,
    environment: filters.environment,
    app_id: filters.appId ?? undefined,
    connection_id: filters.connectionId ?? undefined,
    currency: filters.currency ?? undefined,
  };
}

export function reportsExportUrl(filters: ReportFilters): string {
  return withQuery("/admin/payment-reports/export", reportQuery(filters));
}

export const reportKeys = {
  all: ["payment-reports"] as const,
  summary: (filters: ReportFilters | null, interval: ReportInterval) => [...reportKeys.all, "summary", interval, filters] as const,
  list: (filters: ReportFilters) => [...reportKeys.all, "list", filters] as const,
};

/** `filters` is null while the picked range can't be sent; nothing is requested then. */
export function usePaymentReportSummary(filters: ReportFilters | null, interval: ReportInterval) {
  return useQuery({
    queryKey: reportKeys.summary(filters, interval),
    queryFn: filters
      ? () => unwrap(api.GET("/admin/payment-reports/summary", { params: { query: { ...reportQuery(filters), interval } } }))
      : skipToken,
  });
}

export function usePaymentReports(filters: ReportFilters) {
  return useInfiniteQuery({
    queryKey: reportKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/payment-reports", {
          params: { query: { ...reportQuery(filters), limit: REPORTS_PAGE_SIZE, cursor: cursor ?? undefined } },
        }),
      ),
    ),
  });
}
