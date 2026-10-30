import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import type { Mode } from "@/shared/lib/mode";

export type Dashboard = components["schemas"]["DashboardResponse"];
export type DashboardRange = components["schemas"]["DashboardRange"];
export type DashboardSeriesPoint = components["schemas"]["DashboardSeriesPoint"];
export type DashboardCurrencyTotals = components["schemas"]["DashboardCurrencyTotals"];
export type DashboardPaymentTotals = components["schemas"]["DashboardPaymentTotals"];

export const DASHBOARD_RANGES: readonly DashboardRange[] = ["7d", "30d"];
export const DEFAULT_RANGE: DashboardRange = "7d";

export function isDashboardRange(value: string | null): value is DashboardRange {
  return value !== null && (DASHBOARD_RANGES as readonly string[]).includes(value);
}

/** The number of WIB days a range covers. */
export function rangeDays(range: DashboardRange): number {
  return range === "30d" ? 30 : 7;
}

export interface DashboardQuery {
  range: DashboardRange;
  environment: Mode;
}

export const dashboardKeys = {
  all: ["dashboard"] as const,
  get: (query: DashboardQuery) => [...dashboardKeys.all, query] as const,
};

export function useDashboard(query: DashboardQuery) {
  return useQuery({
    queryKey: dashboardKeys.get(query),
    queryFn: () => unwrap(api.GET("/admin/dashboard", { params: { query: { range: query.range, environment: query.environment } } })),
    staleTime: 30_000,
  });
}
