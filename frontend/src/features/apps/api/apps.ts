import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";

export type App = components["schemas"]["App"];
export type AppSummary = components["schemas"]["AppSummary"];
export type AppEnvironment = components["schemas"]["AppEnvironment"];
export type AppStatus = components["schemas"]["AppStatus"];
export type Metadata = components["schemas"]["Metadata"];
export type CreateAppRequest = components["schemas"]["CreateAppRequest"];
export type UpdateAppRequest = components["schemas"]["UpdateAppRequest"];
export type AppCreated = components["schemas"]["AppCreatedResponse"];
export type ApiKey = components["schemas"]["ApiKey"];
export type ApiKeyStatus = components["schemas"]["ApiKeyStatus"];
export type CreateApiKeyRequest = components["schemas"]["CreateApiKeyRequest"];
export type ApiKeyCreated = components["schemas"]["ApiKeyCreatedResponse"];

export const APP_ENVIRONMENTS: readonly AppEnvironment[] = ["test", "live"];
export const APP_STATUSES: readonly AppStatus[] = ["active", "disabled"];
export const APPS_PAGE_SIZE = 50;
export const KEYS_PAGE_SIZE = 50;
/** The app filter select loads one page of this size; servers with more apps show only the newest. */
export const APP_OPTIONS_LIMIT = 200;

export function isAppEnvironment(value: string | null): value is AppEnvironment {
  return value !== null && (APP_ENVIRONMENTS as readonly string[]).includes(value);
}

export function isAppStatus(value: string | null): value is AppStatus {
  return value !== null && (APP_STATUSES as readonly string[]).includes(value);
}

export interface AppFilters {
  environment: AppEnvironment | null;
  status: AppStatus | null;
}

export const appKeys = {
  all: ["apps"] as const,
  lists: () => [...appKeys.all, "list"] as const,
  list: (filters: AppFilters) => [...appKeys.lists(), filters] as const,
  options: () => [...appKeys.all, "options"] as const,
  detail: (id: string) => [...appKeys.all, "detail", id] as const,
  keys: (id: string) => [...appKeys.detail(id), "keys"] as const,
};

const byApp = (id: string) => ({ params: { path: { id } } });
const byKey = (id: string, keyId: string) => ({ params: { path: { id, key_id: keyId } } });

export function useApps(filters: AppFilters) {
  return useInfiniteQuery({
    queryKey: appKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/apps", {
          params: {
            query: {
              environment: filters.environment ?? undefined,
              status: filters.status ?? undefined,
              limit: APPS_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
  });
}

/** The first `APP_OPTIONS_LIMIT` apps, newest first, for filter selects. */
export function useAppOptions() {
  return useQuery({
    queryKey: appKeys.options(),
    queryFn: async () =>
      (await unwrap(api.GET("/admin/apps", { params: { query: { limit: APP_OPTIONS_LIMIT } } }))).data,
  });
}

export function useApp(id: string) {
  return useQuery({
    queryKey: appKeys.detail(id),
    queryFn: async () => (await unwrap(api.GET("/admin/apps/{id}", byApp(id)))).app,
  });
}

export function useApiKeys(appId: string) {
  return useInfiniteQuery({
    queryKey: appKeys.keys(appId),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/apps/{id}/keys", {
          params: { path: { id: appId }, query: { limit: KEYS_PAGE_SIZE, cursor: cursor ?? undefined } },
        }),
      ),
    ),
  });
}

/** After an app changes: its detail, every list and the filter options. */
function refreshApp(queryClient: QueryClient, app: App) {
  queryClient.setQueryData(appKeys.detail(app.id), app);
  return Promise.all([
    queryClient.invalidateQueries({ queryKey: appKeys.lists() }),
    queryClient.invalidateQueries({ queryKey: appKeys.options() }),
  ]);
}

export function useCreateApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ body, metadataText }: { body: CreateAppRequest; metadataText?: string }) =>
      unwrap(api.POST("/admin/apps", { body, bodySerializer: withRawMetadata(metadataText) })),
    onSuccess: (result) => refreshApp(queryClient, result.app),
  });
}

export function useUpdateApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, body, metadataText }: { id: string; body: UpdateAppRequest; metadataText?: string }) =>
      unwrap(api.PATCH("/admin/apps/{id}", { ...byApp(id), body, bodySerializer: withRawMetadata(metadataText) })),
    onSuccess: (result) => refreshApp(queryClient, result.app),
  });
}

export function useDisableApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/admin/apps/{id}/disable", byApp(id))),
    onSuccess: (result) => refreshApp(queryClient, result.app),
  });
}

export function useEnableApp() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => unwrap(api.POST("/admin/apps/{id}/enable", byApp(id))),
    onSuccess: (result) => refreshApp(queryClient, result.app),
  });
}

function useKeyMutation<TVariables extends { appId: string }, TData>(
  mutationFn: (variables: TVariables) => Promise<TData>,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn,
    onSuccess: (_data, variables) => queryClient.invalidateQueries({ queryKey: appKeys.keys(variables.appId) }),
  });
}

export function useCreateApiKey() {
  return useKeyMutation(({ appId, body }: { appId: string; body: CreateApiKeyRequest }) =>
    unwrap(api.POST("/admin/apps/{id}/keys", { ...byApp(appId), body })),
  );
}

export function useRotateApiKey() {
  return useKeyMutation(({ appId, keyId }: { appId: string; keyId: string }) =>
    unwrap(api.POST("/admin/apps/{id}/keys/{key_id}/rotate", byKey(appId, keyId))),
  );
}

export function useRevokeApiKey() {
  return useKeyMutation(({ appId, keyId }: { appId: string; keyId: string }) =>
    unwrap(api.POST("/admin/apps/{id}/keys/{key_id}/revoke", byKey(appId, keyId))),
  );
}

/**
 * Serializes a request body but sends `metadata` as the text the user typed
 * (already validated as a JSON object). JSON.parse would round integers past
 * 2^53, and the API promises to keep metadata numbers digit for digit.
 */
export function withRawMetadata(metadataText: string | undefined) {
  return (body: unknown): string => {
    const raw = metadataText?.trim();
    if (!raw || typeof body !== "object" || body === null || !("metadata" in body)) return JSON.stringify(body);
    const { metadata: _parsed, ...rest } = body as Record<string, unknown>;
    const head = JSON.stringify(rest);
    return head === "{}" ? `{"metadata":${raw}}` : `${head.slice(0, -1)},"metadata":${raw}}`;
  };
}
