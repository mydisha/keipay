import { useCallback, useState } from "react";
import { useSearchParams } from "react-router";
import { presetRange, type DateRange } from "@/shared/lib/wib-dates";
import { isReportInterval, type ReportInterval } from "../api/reports";
import { isReconciliationResult, isRunStatus, type ItemFilters, type RunStatus } from "../api/runs";

// The Reconciliation pages keep their tab, filters and open item in the
// query string, so a reload or a shared link shows the same view.

export type ReconciliationTab = "summary" | "runs";

export const SUMMARY_FILTER_KEYS = ["app_id", "connection_id", "currency"] as const;

export const runPath = (id: string) => `/reconciliation/runs/${encodeURIComponent(id)}`;
export const RUNS_TAB_PATH = "/reconciliation?tab=runs";

function useQueryState() {
  const [params, setParams] = useSearchParams();
  const update = useCallback(
    (change: (next: URLSearchParams) => void, replace = true) =>
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          change(next);
          return next;
        },
        { replace },
      ),
    [setParams],
  );
  return { params, update };
}

/** `/reconciliation?tab=…&from=YYYY-MM-DD&to=…&interval=…&app_id=…&connection_id=…&currency=…&status=…` */
export function useReconciliationSearch() {
  const { params, update } = useQueryState();
  // The default period is "This month" at the time the page opened.
  const [opened] = useState(() => new Date());
  const fallback = presetRange("this_month", opened);

  const tab: ReconciliationTab = params.get("tab") === "runs" ? "runs" : "summary";
  const rawInterval = params.get("interval");
  const interval: ReportInterval = isReportInterval(rawInterval) ? rawInterval : "total";
  const hasRange = params.has("from") || params.has("to");
  // Dates as typed: an empty or partial input stays visible and is reported, never replaced.
  const range = {
    from: hasRange ? (params.get("from") ?? "") : fallback.from,
    to: hasRange ? (params.get("to") ?? "") : fallback.to,
  };
  const rawStatus = params.get("status");
  const status: RunStatus | null = isRunStatus(rawStatus) ? rawStatus : null;

  const setValue = (key: string, value: string | null) =>
    update((next) => {
      if (value === null) next.delete(key);
      else next.set(key, value);
    });

  return {
    tab,
    interval,
    range,
    appId: params.get("app_id"),
    connectionId: params.get("connection_id"),
    currency: params.get("currency"),
    status,
    opened,
    setTab: (value: ReconciliationTab) => update((next) => next.set("tab", value), false),
    setRange: (value: Partial<DateRange>) =>
      update((next) => {
        next.set("from", value.from ?? range.from);
        next.set("to", value.to ?? range.to);
      }),
    setInterval: (value: ReportInterval) => setValue("interval", value === "total" ? null : value),
    setFilter: (key: (typeof SUMMARY_FILTER_KEYS)[number] | "status", value: string | null) => setValue(key, value),
    clearFilters: (keys: readonly string[]) =>
      update((next) => {
        for (const key of keys) next.delete(key);
      }),
  };
}

export type ReconciliationSearch = ReturnType<typeof useReconciliationSearch>;

const RESOLVED_VALUES: Record<string, boolean> = { resolved: true, unresolved: false };

/** `/reconciliation/runs/:id?result=…&resolved=resolved|unresolved&item=rci_…` */
export function useRunSearch() {
  const { params, update } = useQueryState();
  const rawResult = params.get("result");
  const rawResolved = params.get("resolved");
  const filters: ItemFilters = {
    result: isReconciliationResult(rawResult) ? rawResult : null,
    resolved: rawResolved !== null && rawResolved in RESOLVED_VALUES ? (RESOLVED_VALUES[rawResolved] ?? null) : null,
  };
  return {
    filters,
    itemId: params.get("item"),
    setResult: (value: string | null) =>
      update((next) => {
        if (value === null) next.delete("result");
        else next.set("result", value);
      }),
    setResolved: (value: "resolved" | "unresolved" | null) =>
      update((next) => {
        if (value === null) next.delete("resolved");
        else next.set("resolved", value);
      }),
    clearFilters: () =>
      update((next) => {
        next.delete("result");
        next.delete("resolved");
      }),
    openItem: (id: string) => update((next) => next.set("item", id), false),
    closeItem: () => update((next) => next.delete("item")),
  };
}

export type RunSearch = ReturnType<typeof useRunSearch>;
