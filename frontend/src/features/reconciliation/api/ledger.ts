import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type LedgerEntry = components["schemas"]["LedgerEntry"];
export type LedgerSummary = components["schemas"]["LedgerSummary"];

export function useLedger(tenantId: number | null) {
  return useQuery({
    queryKey: ["ledger", { tenantId }],
    queryFn: () =>
      unwrap(api.GET("/admin/ledger", { params: { query: { tenant_id: tenantId ?? undefined } } })),
    placeholderData: keepPreviousData,
  });
}
