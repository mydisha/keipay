import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type AuditLog = components["schemas"]["AuditLog"];

export const AUDIT_PAGE_SIZE = 50;

export function useAuditLogs(cursor: string | null) {
  return useQuery({
    queryKey: ["audit-logs", { cursor }],
    queryFn: () =>
      unwrap(
        api.GET("/admin/audit-logs", {
          params: { query: { limit: AUDIT_PAGE_SIZE, cursor: cursor ?? undefined } },
        }),
      ),
    placeholderData: keepPreviousData,
  });
}
