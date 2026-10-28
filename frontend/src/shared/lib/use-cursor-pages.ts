import type { InfiniteData, UseInfiniteQueryResult } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { toast } from "sonner";
import { isApiError } from "@/shared/api/errors";
import { strings } from "./strings";

/** The shape of every cursor-paginated list response. */
export interface CursorPage<T> {
  data: T[];
  next_cursor: string | null;
}

/** `useInfiniteQuery` options for a cursor list; `fetchPage(null)` loads the first page. */
export function cursorPageOptions<T>(fetchPage: (cursor: string | null) => Promise<CursorPage<T>>) {
  return {
    queryFn: ({ pageParam }: { pageParam: string | null }) => fetchPage(pageParam),
    initialPageParam: null as string | null,
    getNextPageParam: (last: CursorPage<T>) => last.next_cursor ?? undefined,
  };
}

export interface LoadMoreList<T> {
  /** Every row loaded so far; undefined until the first page arrives. */
  rows: T[] | undefined;
  /** Only a failed first page: later pages fail with a toast and keep the rows. */
  error: unknown;
  loadMore: { hasMore: boolean; loading: boolean; onLoadMore: () => void };
}

/** Flattens an infinite cursor query for DataTable's "Load more". */
export function loadMoreList<T>(
  query: UseInfiniteQueryResult<InfiniteData<CursorPage<T>>>,
): LoadMoreList<T> {
  const { data, error, hasNextPage, isFetchingNextPage, fetchNextPage } = query;
  return {
    rows: data?.pages.flatMap((page) => page.data),
    error: data ? null : error,
    loadMore: {
      hasMore: hasNextPage,
      loading: isFetchingNextPage,
      onLoadMore: () => {
        void fetchNextPage().then((result) => {
          if (result.isFetchNextPageError) toast.error(strings.common.loadMoreFailed);
        });
      },
    },
  };
}

/**
 * A stale or tampered cursor (`invalid_cursor`) goes back to the newest page
 * with a notice instead of an error. Returns `resetting` (show the table as
 * loading meanwhile) and a `resetKey` for DataTable that changes on each reset.
 */
export function useInvalidCursorReset(
  error: unknown,
  cursor: string | null,
  setCursor: (cursor: string | null) => void,
  message: string,
): { resetting: boolean; resetKey: string } {
  const [resetCount, setResetCount] = useState(0);
  const resetting = isApiError(error) && error.code === "invalid_cursor" && cursor !== null;

  useEffect(() => {
    if (!resetting) return;
    toast.info(message);
    setCursor(null);
    setResetCount((count) => count + 1);
  }, [resetting, message, setCursor]);

  return { resetting, resetKey: String(resetCount) };
}
