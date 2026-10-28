import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type Payment = components["schemas"]["Payment"];
export type PaymentStatus = components["schemas"]["PaymentStatus"];

export const PAYMENT_STATUSES: readonly PaymentStatus[] = ["PENDING", "PAID", "SETTLED", "EXPIRED", "FAILED"];

export function isPaymentStatus(value: string | null): value is PaymentStatus {
  return value !== null && (PAYMENT_STATUSES as readonly string[]).includes(value);
}

export interface PaymentFilters {
  status: PaymentStatus | null;
  tenantId: number | null;
}

export function usePayments(filters: PaymentFilters) {
  return useQuery({
    queryKey: ["payments", filters],
    queryFn: () =>
      unwrap(
        api.GET("/admin/payments", {
          params: {
            query: {
              status: filters.status ?? undefined,
              tenant_id: filters.tenantId ?? undefined,
            },
          },
        }),
      ),
    placeholderData: keepPreviousData,
  });
}
