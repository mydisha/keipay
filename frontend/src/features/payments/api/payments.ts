import { useInfiniteQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";

export type Payment = components["schemas"]["Payment"];
export type PaymentStatus = components["schemas"]["PaymentStatus"];

export const PAYMENT_STATUSES: readonly PaymentStatus[] = ["PENDING", "PAID", "SETTLED", "EXPIRED", "FAILED"];

export function isPaymentStatus(value: string | null): value is PaymentStatus {
  return value !== null && (PAYMENT_STATUSES as readonly string[]).includes(value);
}

export const PAYMENTS_PAGE_SIZE = 50;

export interface PaymentFilters {
  status: PaymentStatus | null;
  /** App public id (`app_…`). */
  appId: string | null;
}

export function usePayments(filters: PaymentFilters) {
  return useInfiniteQuery({
    queryKey: ["payments", filters],
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/payments", {
          params: {
            query: {
              status: filters.status ?? undefined,
              app_id: filters.appId ?? undefined,
              limit: PAYMENTS_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
  });
}
