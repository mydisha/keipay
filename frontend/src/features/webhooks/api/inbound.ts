import { keepPreviousData, useInfiniteQuery, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";
import type { Environment } from "./deliveries";

export type InboundWebhook = components["schemas"]["InboundWebhook"];
export type InboundWebhookDetail = components["schemas"]["InboundWebhookDetail"];
export type InboundResult = components["schemas"]["InboundWebhookResult"];

export const INBOUND_RESULTS: readonly InboundResult[] = [
  "applied",
  "stale",
  "duplicate",
  "mismatch",
  "unmapped",
  "invalid_signature",
  "invalid_payload",
  "error",
];
export const INBOUND_PAGE_SIZE = 50;

export function isInboundResult(value: string | null): value is InboundResult {
  return value !== null && (INBOUND_RESULTS as readonly string[]).includes(value);
}

export interface InboundFilters {
  result: InboundResult | null;
  connectionId: string | null;
  environment: Environment | null;
}

export const inboundKeys = {
  all: ["inbound-webhooks"] as const,
  list: (filters: InboundFilters) => [...inboundKeys.all, "list", filters] as const,
  detail: (id: string) => [...inboundKeys.all, "detail", id] as const,
};

export function useInboundWebhooks(filters: InboundFilters) {
  return useInfiniteQuery({
    queryKey: inboundKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/inbound-webhooks", {
          params: {
            query: {
              result: filters.result ?? undefined,
              connection_id: filters.connectionId ?? undefined,
              environment: filters.environment ?? undefined,
              limit: INBOUND_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
    placeholderData: keepPreviousData,
  });
}

export function useInboundWebhook(id: string | null) {
  return useQuery({
    queryKey: inboundKeys.detail(id ?? ""),
    enabled: id !== null,
    queryFn: async () =>
      (await unwrap(api.GET("/admin/inbound-webhooks/{id}", { params: { path: { id: id ?? "" } } }))).inbound_webhook,
  });
}
