import { useInfiniteQuery, useMutation, useQuery, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions } from "@/shared/lib/use-cursor-pages";

export type Provider = components["schemas"]["Provider"];
export type SchemaField = components["schemas"]["SchemaField"];
export type Connection = components["schemas"]["Connection"];
export type ConnectionListItem = components["schemas"]["ConnectionListItem"];
export type ConnectionSummary = components["schemas"]["ConnectionSummary"];
export type ConnectionStatus = components["schemas"]["ConnectionStatus"];
export type ConnectionChannel = components["schemas"]["ConnectionChannel"];
export type ConnectionEnvironment = components["schemas"]["AppEnvironment"];
export type ConnectionTestResult = components["schemas"]["ConnectionTestResponse"];
export type CreateConnectionRequest = components["schemas"]["CreateConnectionRequest"];
export type UpdateConnectionRequest = components["schemas"]["UpdateConnectionRequest"];
export type AppSummary = components["schemas"]["AppSummary"];

export const CONNECTION_ENVIRONMENTS: readonly ConnectionEnvironment[] = ["test", "live"];
export const CONNECTION_STATUSES: readonly ConnectionStatus[] = ["active", "disabled"];
export const CONNECTIONS_PAGE_SIZE = 50;
/** The default-connection select loads one page of this size (the API maximum). */
export const CONNECTION_OPTIONS_LIMIT = 200;

export function isConnectionEnvironment(value: string | null): value is ConnectionEnvironment {
  return value !== null && (CONNECTION_ENVIRONMENTS as readonly string[]).includes(value);
}

export function isConnectionStatus(value: string | null): value is ConnectionStatus {
  return value !== null && (CONNECTION_STATUSES as readonly string[]).includes(value);
}

export interface ConnectionFilters {
  environment: ConnectionEnvironment | null;
  status: ConnectionStatus | null;
  provider: string | null;
}

export const providerKeys = {
  all: ["providers"] as const,
};

export const connectionKeys = {
  all: ["connections"] as const,
  lists: () => [...connectionKeys.all, "list"] as const,
  list: (filters: ConnectionFilters) => [...connectionKeys.lists(), filters] as const,
  options: (environment: ConnectionEnvironment) => [...connectionKeys.all, "options", environment] as const,
  detail: (id: string) => [...connectionKeys.all, "detail", id] as const,
};

const byConnection = (id: string) => ({ params: { path: { id } } });

/** Every provider the server has, with the schemas the forms render from. Fixed per server version. */
export function useProviders() {
  return useQuery({
    queryKey: providerKeys.all,
    queryFn: async () => (await unwrap(api.GET("/admin/providers"))).data,
    staleTime: Infinity,
  });
}

/** The provider's `display_name`, or its raw name until the providers load or when it is unknown. */
export function providerLabel(providers: Provider[] | undefined, name: string): string {
  return providers?.find((p) => p.name === name)?.display_name ?? name;
}

export function useConnections(filters: ConnectionFilters) {
  return useInfiniteQuery({
    queryKey: connectionKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/connections", {
          params: {
            query: {
              environment: filters.environment ?? undefined,
              status: filters.status ?? undefined,
              provider: filters.provider ?? undefined,
              limit: CONNECTIONS_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
  });
}

/** Active connections of one environment: the choices for an app's default connection. */
export function useConnectionOptions(environment: ConnectionEnvironment, enabled = true) {
  return useQuery({
    queryKey: connectionKeys.options(environment),
    enabled,
    queryFn: async () =>
      (
        await unwrap(
          api.GET("/admin/connections", {
            params: { query: { environment, status: "active", limit: CONNECTION_OPTIONS_LIMIT } },
          }),
        )
      ).data,
  });
}

/** Every connection, newest first, for filter selects (one page of `CONNECTION_OPTIONS_LIMIT`). */
export function useConnectionFilterOptions() {
  return useQuery({
    queryKey: [...connectionKeys.all, "filter-options"] as const,
    queryFn: async () =>
      (await unwrap(api.GET("/admin/connections", { params: { query: { limit: CONNECTION_OPTIONS_LIMIT } } }))).data,
  });
}

export function useConnection(id: string | null) {
  return useQuery({
    queryKey: connectionKeys.detail(id ?? ""),
    enabled: id !== null,
    queryFn: async () => (await unwrap(api.GET("/admin/connections/{id}", byConnection(id ?? "")))).connection,
  });
}

/** After a connection changes: its detail, every list and the option lists. */
function refreshConnection(queryClient: QueryClient, connection: Connection) {
  queryClient.setQueryData(connectionKeys.detail(connection.id), connection);
  return queryClient.invalidateQueries({
    queryKey: connectionKeys.all,
    predicate: (query) => query.queryKey[1] !== "detail",
  });
}

function useConnectionMutation<TVariables, TData extends { connection: Connection }>(
  mutate: (variables: TVariables) => Promise<TData>,
) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: mutate,
    onSuccess: (result) => refreshConnection(queryClient, result.connection),
  });
}

export function useCreateConnection() {
  return useConnectionMutation((body: CreateConnectionRequest) => unwrap(api.POST("/admin/connections", { body })));
}

export function useUpdateConnection() {
  return useConnectionMutation(({ id, body }: { id: string; body: UpdateConnectionRequest }) =>
    unwrap(api.PATCH("/admin/connections/{id}", { ...byConnection(id), body })),
  );
}

export function useReplaceCredentials() {
  return useConnectionMutation(({ id, credentials }: { id: string; credentials: Record<string, string> }) =>
    unwrap(api.PATCH("/admin/connections/{id}/credentials", { ...byConnection(id), body: { credentials } })),
  );
}

export function useReplaceChannels() {
  return useConnectionMutation(
    ({ id, channels }: { id: string; channels: { channel: string; enabled: boolean }[] }) =>
      unwrap(api.PUT("/admin/connections/{id}/channels", { ...byConnection(id), body: { channels } })),
  );
}

export function useTestConnection() {
  return useConnectionMutation((id: string) => unwrap(api.POST("/admin/connections/{id}/test", byConnection(id))));
}

export function useDisableConnection() {
  return useConnectionMutation((id: string) => unwrap(api.POST("/admin/connections/{id}/disable", byConnection(id))));
}

export function useEnableConnection() {
  return useConnectionMutation((id: string) => unwrap(api.POST("/admin/connections/{id}/enable", byConnection(id))));
}
