import { keepPreviousData, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type WebhookDelivery = components["schemas"]["WebhookDelivery"];
export type DeliveryStatus = components["schemas"]["WebhookDeliveryStatus"];

export const DELIVERY_STATUSES: readonly DeliveryStatus[] = ["PENDING", "DELIVERED", "FAILED"];

export function isDeliveryStatus(value: string | null): value is DeliveryStatus {
  return value !== null && (DELIVERY_STATUSES as readonly string[]).includes(value);
}

export interface DeliveryFilters {
  status: DeliveryStatus | null;
  /** App public id (`app_…`). */
  appId: string | null;
}

const deliveryKeys = { all: ["webhook-deliveries"] as const };

export function useDeliveries(filters: DeliveryFilters) {
  return useQuery({
    queryKey: [...deliveryKeys.all, filters],
    queryFn: async () =>
      (
        await unwrap(
          api.GET("/admin/webhook-deliveries", {
            params: {
              query: {
                status: filters.status ?? undefined,
                app_id: filters.appId ?? undefined,
              },
            },
          }),
        )
      ).deliveries,
    placeholderData: keepPreviousData,
  });
}

export function useReplayDelivery() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: number) =>
      unwrap(api.POST("/admin/webhook-deliveries/{id}/replay", { params: { path: { id } } })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.all }),
  });
}
