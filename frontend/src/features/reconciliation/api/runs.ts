import {
  skipToken,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";
import { cursorPageOptions, type CursorPage } from "@/shared/lib/use-cursor-pages";
import { withQuery } from "./reports";

export type ReconciliationRun = components["schemas"]["ReconciliationRun"];
export type RunStatus = components["schemas"]["ReconciliationRunStatus"];
export type ReconciliationItem = components["schemas"]["ReconciliationItem"];
export type ReconciliationResult = components["schemas"]["ReconciliationResult"];
export type CreateRunRequest = components["schemas"]["CreateReconciliationRunRequest"];
type Environment = components["schemas"]["AppEnvironment"];

export const RUN_STATUSES: readonly RunStatus[] = ["queued", "running", "succeeded", "failed"];
/** Display order: the spec's enum order (matched first, then by precedence). */
export const RECONCILIATION_RESULTS: readonly ReconciliationResult[] = [
  "matched",
  "missing_in_keipay",
  "missing_in_gateway",
  "amount_mismatch",
  "status_mismatch",
  "fee_mismatch",
];
export const RUNS_PAGE_SIZE = 50;
export const ITEMS_PAGE_SIZE = 100;
/** How often the runs list reloads while a visible run is queued or running. */
export const RUNS_POLL_MS = 3000;
/** How often an open run reloads while it is queued or running. */
export const RUN_POLL_MS = 2000;

export function isRunStatus(value: string | null): value is RunStatus {
  return value !== null && (RUN_STATUSES as readonly string[]).includes(value);
}

export function isReconciliationResult(value: string | null): value is ReconciliationResult {
  return value !== null && (RECONCILIATION_RESULTS as readonly string[]).includes(value);
}

export function isRunActive(status: RunStatus | undefined): boolean {
  return status === "queued" || status === "running";
}

/** The runs list keeps polling while any loaded run is still working. */
export function runsRefetchInterval(data: InfiniteData<CursorPage<ReconciliationRun>> | undefined): number | false {
  const active = data?.pages.some((page) => page.data.some((run) => isRunActive(run.status))) ?? false;
  return active ? RUNS_POLL_MS : false;
}

export function runRefetchInterval(run: ReconciliationRun | undefined): number | false {
  return isRunActive(run?.status) ? RUN_POLL_MS : false;
}

export interface RunFilters {
  status: RunStatus | null;
  environment: Environment;
}

export interface ItemFilters {
  result: ReconciliationResult | null;
  /** true: resolved only; false: unresolved only (matched included); null: all. */
  resolved: boolean | null;
}

export const runKeys = {
  all: ["reconciliation-runs"] as const,
  lists: () => [...runKeys.all, "list"] as const,
  list: (filters: RunFilters) => [...runKeys.lists(), filters] as const,
  detail: (id: string) => [...runKeys.all, "detail", id] as const,
  itemLists: () => [...runKeys.all, "items"] as const,
  items: (runId: string) => [...runKeys.itemLists(), runId] as const,
  itemList: (runId: string, runCount: number, filters: ItemFilters) =>
    [...runKeys.items(runId), runCount, filters] as const,
  itemDetails: () => [...runKeys.all, "item"] as const,
  item: (id: string) => [...runKeys.itemDetails(), id] as const,
};

export function useReconciliationRuns(filters: RunFilters) {
  return useInfiniteQuery({
    queryKey: runKeys.list(filters),
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/reconciliation-runs", {
          params: {
            query: {
              status: filters.status ?? undefined,
              environment: filters.environment,
              limit: RUNS_PAGE_SIZE,
              cursor: cursor ?? undefined,
            },
          },
        }),
      ),
    ),
    refetchInterval: (query) => runsRefetchInterval(query.state.data),
  });
}

const byRun = (id: string) => ({ params: { path: { id } } });

export function useReconciliationRun(id: string) {
  return useQuery({
    queryKey: runKeys.detail(id),
    queryFn: async () => (await unwrap(api.GET("/admin/reconciliation-runs/{id}", byRun(id)))).run,
    refetchInterval: (query) => runRefetchInterval(query.state.data),
  });
}

function itemQuery(filters: ItemFilters) {
  return { result: filters.result ?? undefined, resolved: filters.resolved ?? undefined };
}

/** Items of a finished run; the key carries `run_count` so a re-run never shows the previous items. */
export function useRunItems(run: ReconciliationRun, filters: ItemFilters) {
  return useInfiniteQuery({
    queryKey: runKeys.itemList(run.id, run.run_count, filters),
    enabled: run.status === "succeeded",
    ...cursorPageOptions((cursor) =>
      unwrap(
        api.GET("/admin/reconciliation-runs/{id}/items", {
          params: {
            path: { id: run.id },
            query: { ...itemQuery(filters), limit: ITEMS_PAGE_SIZE, cursor: cursor ?? undefined },
          },
        }),
      ),
    ),
  });
}

type ItemPages = InfiniteData<CursorPage<ReconciliationItem>>;

function loadedItem(queryClient: QueryClient, id: string): ReconciliationItem | undefined {
  for (const [, data] of queryClient.getQueriesData<ItemPages>({ queryKey: runKeys.itemLists() })) {
    const found = data?.pages.flatMap((page) => page.data).find((row) => row.id === id);
    if (found) return found;
  }
  return undefined;
}

/**
 * One item by id, whatever the list filters. A row already loaded in a list
 * shows at once as a placeholder while the read confirms it.
 */
export function useReconciliationItem(id: string | null) {
  const queryClient = useQueryClient();
  return useQuery({
    queryKey: runKeys.item(id ?? ""),
    queryFn: id
      ? async () =>
          (await unwrap(api.GET("/admin/reconciliation-items/{id}", { params: { path: { id } } }))).item
      : skipToken,
    placeholderData: () => (id ? loadedItem(queryClient, id) : undefined),
  });
}

export function itemsExportUrl(runId: string, filters: ItemFilters): string {
  return withQuery(`/admin/reconciliation-runs/${encodeURIComponent(runId)}/items/export`, itemQuery(filters));
}

function storeRun(queryClient: QueryClient, run: ReconciliationRun) {
  queryClient.setQueryData(runKeys.detail(run.id), run);
  return queryClient.invalidateQueries({ queryKey: runKeys.lists() });
}

export function useCreateRun() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (body: CreateRunRequest) => (await unwrap(api.POST("/admin/reconciliation-runs", { body }))).run,
    onSuccess: (run) => storeRun(queryClient, run),
  });
}

export function useRerun() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (id: string) => (await unwrap(api.POST("/admin/reconciliation-runs/{id}/rerun", byRun(id)))).run,
    onSuccess: (run) => {
      // A re-run replaces the run's items, so any item read before is gone.
      queryClient.removeQueries({ queryKey: runKeys.itemDetails() });
      return storeRun(queryClient, run);
    },
  });
}

/**
 * After a resolve: the item is replaced in its own read and in every loaded
 * list of its run (it stays where it is, even under an "Unresolved" filter,
 * until the next load), and the run reloads for its counts.
 */
export function useResolveItem() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async ({ id, note }: { id: string; note: string }) =>
      (await unwrap(api.POST("/admin/reconciliation-items/{id}/resolve", { params: { path: { id } }, body: { note } })))
        .item,
    onSuccess: (item) => {
      queryClient.setQueryData(runKeys.item(item.id), item);
      queryClient.setQueriesData<ItemPages>({ queryKey: runKeys.items(item.run_id) }, (data) =>
        data
          ? {
              ...data,
              pages: data.pages.map((page) => ({
                ...page,
                data: page.data.map((row) => (row.id === item.id ? item : row)),
              })),
            }
          : data,
      );
      // Not awaited: the sheet stays locked while the resolve is pending, and the
      // item is already written, so a slow reload of the counts must not hold it.
      void queryClient.invalidateQueries({ queryKey: runKeys.detail(item.run_id) });
      void queryClient.invalidateQueries({ queryKey: runKeys.lists() });
    },
  });
}

export type ResolveItemMutation = ReturnType<typeof useResolveItem>;
