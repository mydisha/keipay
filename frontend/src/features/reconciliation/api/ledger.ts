import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type LedgerEntry = components["schemas"]["LedgerEntry"];
export type LedgerSummary = components["schemas"]["LedgerSummary"];

/** `appId` is an app public id (`app_…`), or null for every app. */
export function useLedger(appId: string | null) {
  return useQuery({
    queryKey: ["ledger", { appId }],
    queryFn: () => unwrap(api.GET("/admin/ledger", { params: { query: { app_id: appId ?? undefined } } })),
    placeholderData: keepPreviousData,
  });
}
