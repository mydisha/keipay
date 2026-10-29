import { keepPreviousData, useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";
import type { EventType } from "./event-types";

export type WebhookDelivery = components["schemas"]["WebhookDelivery"];
export type WebhookDeliveryDetail = components["schemas"]["WebhookDeliveryDetail"];
export type WebhookDeliveryAttempt = components["schemas"]["WebhookDeliveryAttempt"];
export type DeliveryStatus = components["schemas"]["WebhookDeliveryStatus"];
export type Environment = components["schemas"]["AppEnvironment"];

export const DELIVERY_STATUSES: readonly DeliveryStatus[] = ["PENDING", "DELIVERED", "FAILED"];
export const DELIVERIES_PAGE_SIZE = 50;
/** How often an open pending delivery is reloaded, so its next attempt shows up. */
const PENDING_REFRESH_MS = 3000;

export function isDeliveryStatus(value: string | null): value is DeliveryStatus {
  return value !== null && (DELIVERY_STATUSES as readonly string[]).includes(value);
}

export interface DeliveryFilters {
  status: DeliveryStatus | null;
  eventType: EventType | null;
  /** App public id (`app_…`). */
  appId: string | null;
  endpointId: string | null;
  eventId: string | null;
  environment: Environment | null;
}

export const deliveryKeys = {
  all: ["webhook-deliveries"] as const,
  lists: () => [...deliveryKeys.all, "list"] as const,
  list: (filters: DeliveryFilters) => [...deliveryKeys.lists(), filters] as const,
  detail: (id: string) => [...deliveryKeys.all, "detail", id] as const,
};

export function useDeliveries(filters: DeliveryFilters) {
  return useInfiniteQuery({
    queryKey: deliveryKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/webhook-deliveries", {
          params: {
            query: {
              status: filters.status ?? undefined,
              event_type: filters.eventType ?? undefined,
              app_id: filters.appId ?? undefined,
              endpoint_id: filters.endpointId ?? undefined,
              event_id: filters.eventId ?? undefined,
              environment: filters.environment ?? undefined,
              limit: DELIVERIES_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
    placeholderData: keepPreviousData,
  });
}

export function useDelivery(id: string | null) {
  return useQuery({
    queryKey: deliveryKeys.detail(id ?? ""),
    enabled: id !== null,
    queryFn: async () =>
      (await unwrap(api.GET("/admin/webhook-deliveries/{id}", { params: { path: { id: id ?? "" } } }))).delivery,
    refetchInterval: (query) => (query.state.data?.status === "PENDING" ? PENDING_REFRESH_MS : false),
  });
}

export function useReplayDelivery() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/admin/webhook-deliveries/{id}/replay", { params: { path: { id } } })),
    onSuccess: (_result, id) =>
      Promise.all([
        queryClient.invalidateQueries({ queryKey: deliveryKeys.detail(id) }),
        queryClient.invalidateQueries({ queryKey: deliveryKeys.lists() }),
      ]),
  });
}
