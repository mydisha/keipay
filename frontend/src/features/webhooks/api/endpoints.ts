import { useInfiniteQuery, useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, unwrap, unwrapEmpty } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";
import { deliveryKeys } from "./deliveries";
import type { EventType } from "./event-types";

export type WebhookEndpoint = components["schemas"]["WebhookEndpoint"];
export type WebhookApiVersion = components["schemas"]["WebhookApiVersion"];
export type WebhookRetryPolicy = components["schemas"]["WebhookRetryPolicy"];
export type WebhookHeaderInput = components["schemas"]["WebhookHeaderInput"];
export type CreateWebhookEndpointRequest = components["schemas"]["CreateWebhookEndpointRequest"];
export type UpdateWebhookEndpointRequest = components["schemas"]["UpdateWebhookEndpointRequest"];
export type WebhookEndpointSecret = components["schemas"]["WebhookEndpointSecretResponse"];

export const ENDPOINTS_PAGE_SIZE = 50;
export const WEBHOOK_API_VERSIONS: readonly WebhookApiVersion[] = ["v2", "v1"];
export const DEFAULT_RETRY_POLICY: WebhookRetryPolicy = { max_attempts: 8, backoff: "exponential", interval_seconds: 30 };

export const endpointKeys = {
  all: ["webhook-endpoints"] as const,
  list: (appId: string) => [...endpointKeys.all, "list", appId] as const,
  detail: (id: string) => [...endpointKeys.all, "detail", id] as const,
};

const byEndpoint = (id: string) => ({ params: { path: { id } } });

export function useWebhookEndpoints(appId: string) {
  return useInfiniteQuery({
    queryKey: endpointKeys.list(appId),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/apps/{id}/webhook-endpoints", {
          params: { path: { id: appId }, query: { limit: ENDPOINTS_PAGE_SIZE, cursor: cursor ?? undefined } },
        }),
      ),
    ),
  });
}

/** For the command palette: which app a `whe_` id belongs to. */
export function fetchWebhookEndpoint(queryClient: QueryClient, id: string) {
  return queryClient.fetchQuery({
    queryKey: endpointKeys.detail(id),
    queryFn: async () => (await unwrap(api.GET("/admin/webhook-endpoints/{id}", byEndpoint(id)))).endpoint,
  });
}

function refreshEndpoint(queryClient: QueryClient, endpoint: WebhookEndpoint) {
  queryClient.setQueryData(endpointKeys.detail(endpoint.id), endpoint);
  return queryClient.invalidateQueries({ queryKey: endpointKeys.list(endpoint.app.id) });
}

function useEndpointMutation<TVariables, TData extends { endpoint: WebhookEndpoint }>(
  mutationFn: (variables: TVariables) => Promise<TData>,
) {
  const queryClient = useQueryClient();
  return useMutation({ mutationFn, onSuccess: (result) => refreshEndpoint(queryClient, result.endpoint) });
}

export function useCreateWebhookEndpoint() {
  return useEndpointMutation(({ appId, body }: { appId: string; body: CreateWebhookEndpointRequest }) =>
    unwrap(api.POST("/admin/apps/{id}/webhook-endpoints", { params: { path: { id: appId } }, body })),
  );
}

export function useUpdateWebhookEndpoint() {
  return useEndpointMutation(({ id, body }: { id: string; body: UpdateWebhookEndpointRequest }) =>
    unwrap(api.PATCH("/admin/webhook-endpoints/{id}", { ...byEndpoint(id), body })),
  );
}

export function useRotateWebhookSecret() {
  return useEndpointMutation(({ id, overlapHours }: { id: string; overlapHours: number }) =>
    unwrap(api.POST("/admin/webhook-endpoints/{id}/rotate-secret", { ...byEndpoint(id), body: { overlap_hours: overlapHours } })),
  );
}

export function useDisableWebhookEndpoint() {
  return useEndpointMutation((id: string) => unwrap(api.POST("/admin/webhook-endpoints/{id}/disable", byEndpoint(id))));
}

export function useEnableWebhookEndpoint() {
  return useEndpointMutation((id: string) => unwrap(api.POST("/admin/webhook-endpoints/{id}/enable", byEndpoint(id))));
}

export function useDeleteWebhookEndpoint() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (endpoint: WebhookEndpoint) => unwrapEmpty(api.DELETE("/admin/webhook-endpoints/{id}", byEndpoint(endpoint.id))),
    onSuccess: (_data, endpoint) => {
      queryClient.removeQueries({ queryKey: endpointKeys.detail(endpoint.id) });
      return queryClient.invalidateQueries({ queryKey: endpointKeys.list(endpoint.app.id) });
    },
  });
}

export function useSendTestEvent() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, eventType }: { id: string; eventType: EventType }) =>
      unwrap(api.POST("/admin/webhook-endpoints/{id}/test", { ...byEndpoint(id), body: { event_type: eventType } })),
    onSuccess: () => queryClient.invalidateQueries({ queryKey: deliveryKeys.lists() }),
  });
}
