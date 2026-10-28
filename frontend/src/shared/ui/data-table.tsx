import { flexRender, getCoreRowModel, useReactTable, type ColumnDef } from "@tanstack/react-table";
import { ChevronLeftIcon, ChevronRightIcon, Loader2Icon, XIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { useIsMobile } from "@/shared/lib/use-media-query";
import { cn } from "@/shared/lib/utils";
import { Button } from "./button";
import { ErrorState } from "./error-state";
import { Skeleton } from "./skeleton";

export interface DataColumn<T> {
  id: string;
  header: string;
  cell: (row: T) => ReactNode;
  align?: "left" | "right";
  className?: string;
  /** Card layout below 768px: the card heading, the action row, or left out. */
  card?: "title" | "actions" | "hidden";
}

export interface FilterOption {
  value: string;
  label: string;
}

/** A single-choice chip group; clicking the active chip clears it. */
export interface FilterChipGroup {
  id: string;
  label: string;
  options: FilterOption[];
  value: string | null;
  onChange: (value: string | null) => void;
}

export interface CursorPagination {
  /** `next_cursor` of the page on screen; null or undefined on the last page. */
  nextCursor: string | null | undefined;
  /** Called with the cursor to load; null means the first page. */
  onPageChange: (cursor: string | null) => void;
}

/** "Load more" below the rows, for lists that append the next cursor page. */
export interface LoadMorePagination {
  hasMore: boolean;
  loading: boolean;
  onLoadMore: () => void;
}

export interface DataTableProps<T> {
  columns: DataColumn<T>[];
  data: T[] | undefined;
  getRowId: (row: T) => string;
  /** Accessible name of the table. */
  caption: string;
  isLoading?: boolean;
  error?: unknown;
  onRetry?: () => void;
  emptyState: ReactNode;
  filters?: FilterChipGroup[];
  /** Extra filter controls (e.g. an app select) shown next to the chips. */
  toolbar?: ReactNode;
  /** True when any filter, including toolbar ones, is set. Defaults to the chip state. */
  hasActiveFilters?: boolean;
  onClearFilters?: () => void;
  pagination?: CursorPagination;
  loadMore?: LoadMorePagination;
  /** Changing this value returns cursor pagination to the first page. */
  resetKey?: string;
  onRowClick?: (row: T) => void;
  footer?: ReactNode;
  skeletonRows?: number;
}

// One stable empty array: TanStack Table resets its page index whenever `data`
// changes identity, and a fresh `[]` on every render while loading turned that
// into an endless render loop.
const NO_ROWS: never[] = [];

export function DataTable<T>({
  columns,
  data,
  getRowId,
  caption,
  isLoading = false,
  error,
  onRetry,
  emptyState,
  filters,
  toolbar,
  hasActiveFilters,
  onClearFilters,
  pagination,
  loadMore,
  resetKey,
  onRowClick,
  footer,
  skeletonRows = 5,
}: DataTableProps<T>) {
  const isMobile = useIsMobile();
  const [cursorStack, setCursorStack] = useState<(string | null)[]>([null]);
  const page = cursorStack.length;

  const resetPaging = () => {
    if (!pagination || cursorStack.length === 1) return;
    setCursorStack([null]);
    pagination.onPageChange(null);
  };

  const lastResetKey = useRef(resetKey);
  useEffect(() => {
    if (lastResetKey.current === resetKey) return;
    lastResetKey.current = resetKey;
    setCursorStack([null]);
  }, [resetKey]);

  const tableColumns = useMemo<ColumnDef<T>[]>(
    () =>
      columns.map((column) => ({
        id: column.id,
        header: column.header,
        cell: ({ row }) => column.cell(row.original),
      })),
    [columns],
  );

  const table = useReactTable({
    data: data ?? NO_ROWS,
    // Paging is the server's (cursor or Load more), never the table's.
    manualPagination: true,
    columns: tableColumns,
    getCoreRowModel: getCoreRowModel(),
    getRowId: (row) => getRowId(row),
  });

  const chipsActive = filters?.some((group) => group.value !== null) ?? false;
  const filtersActive = hasActiveFilters ?? chipsActive;

  function clearFilters() {
    resetPaging();
    if (onClearFilters) onClearFilters();
    else filters?.forEach((group) => group.onChange(null));
  }

  function toggleChip(group: FilterChipGroup, value: string) {
    resetPaging();
    group.onChange(group.value === value ? null : value);
  }

  function goNext() {
    if (!pagination?.nextCursor) return;
    const next = pagination.nextCursor;
    setCursorStack((stack) => [...stack, next]);
    pagination.onPageChange(next);
  }

  function goPrevious() {
    if (!pagination || cursorStack.length <= 1) return;
    const stack = cursorStack.slice(0, -1);
    setCursorStack(stack);
    pagination.onPageChange(stack[stack.length - 1] ?? null);
  }

  const rows = table.getRowModel().rows;
  const showError = error !== undefined && error !== null && !isLoading;
  const showEmpty = !isLoading && !showError && rows.length === 0;

  let body: ReactNode;
  if (isLoading) {
    body = isMobile ? (
      <CardSkeleton count={skeletonRows} />
    ) : (
      <TableShell columns={columns} caption={caption} busy>
        {Array.from({ length: skeletonRows }, (_, i) => (
          <tr key={i} data-testid="skeleton-row" className="border-b last:border-0">
            {columns.map((column) => (
              <td key={column.id} className="px-3 py-2.5">
                <Skeleton className="h-4 w-full max-w-32" />
              </td>
            ))}
          </tr>
        ))}
      </TableShell>
    );
  } else if (showError) {
    body = <ErrorState error={error} onRetry={onRetry} />;
  } else if (showEmpty) {
    body = emptyState;
  } else if (isMobile) {
    body = (
      <ul aria-label={caption} data-testid="data-cards" className="grid gap-3">
        {rows.map((row) => (
          <li key={row.id}>
            <RowCard columns={columns} row={row.original} />
          </li>
        ))}
      </ul>
    );
  } else {
    body = (
      <TableShell columns={columns} caption={caption}>
        {rows.map((row) => (
          <tr
            key={row.id}
            onClick={onRowClick ? () => onRowClick(row.original) : undefined}
            className={cn("border-b transition-colors last:border-0 hover:bg-muted/50", onRowClick && "cursor-pointer")}
          >
            {row.getVisibleCells().map((cell, index) => {
              const column = columns[index];
              return (
                <td
                  key={cell.id}
                  className={cn("px-3 py-2.5 align-middle", column?.align === "right" && "text-right", column?.className)}
                  onClick={column?.card === "actions" ? (event) => event.stopPropagation() : undefined}
                >
                  {flexRender(cell.column.columnDef.cell, cell.getContext())}
                </td>
              );
            })}
          </tr>
        ))}
      </TableShell>
    );
  }

  const hasPrevious = page > 1;
  const hasNext = Boolean(pagination?.nextCursor);
  const showLoadMore = Boolean(loadMore?.hasMore) && !isLoading && !showError && !showEmpty;

  return (
    <div className="grid min-w-0 grid-cols-1 gap-3">
      {(filters?.length || toolbar || filtersActive) && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
          {filters?.map((group) => (
            <div key={group.id} role="group" aria-label={strings.common.filterBy(group.label)} className="flex flex-wrap items-center gap-1.5">
              <span className="text-xs font-medium text-muted-foreground">{group.label}</span>
              {group.options.map((option) => {
                const selected = group.value === option.value;
                return (
                  <button
                    key={option.value}
                    type="button"
                    aria-pressed={selected}
                    onClick={() => toggleChip(group, option.value)}
                    className={cn(
                      "inline-flex h-7 items-center rounded-full border px-3 text-xs font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11 max-md:px-4",
                      selected
                        ? "border-primary bg-primary text-primary-foreground"
                        : "border-input bg-card text-foreground hover:bg-accent",
                    )}
                  >
                    {option.label}
                  </button>
                );
              })}
            </div>
          ))}
          {toolbar}
          {filtersActive && (
            <Button variant="ghost" size="sm" onClick={clearFilters}>
              <XIcon aria-hidden="true" />
              {strings.common.clearFilters}
            </Button>
          )}
        </div>
      )}

      {body}

      {showLoadMore && loadMore && (
        <div className="flex justify-center">
          <Button variant="outline" onClick={loadMore.onLoadMore} disabled={loadMore.loading}>
            {loadMore.loading && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {loadMore.loading ? strings.common.loadingMore : strings.common.loadMore}
          </Button>
        </div>
      )}

      {(footer || (pagination && !showError && (hasPrevious || hasNext))) && (
        <div className="flex flex-wrap items-center justify-between gap-2 text-sm text-muted-foreground">
          <div>{footer}</div>
          {pagination && (hasPrevious || hasNext) && (
            <nav aria-label={caption} className="flex items-center gap-2">
              <span aria-live="polite">{strings.common.page(page)}</span>
              <Button variant="outline" size="sm" onClick={goPrevious} disabled={!hasPrevious || isLoading}>
                <ChevronLeftIcon aria-hidden="true" />
                {strings.common.previous}
              </Button>
              <Button variant="outline" size="sm" onClick={goNext} disabled={!hasNext || isLoading}>
                {strings.common.next}
                <ChevronRightIcon aria-hidden="true" />
              </Button>
            </nav>
          )}
        </div>
      )}
    </div>
  );
}

function TableShell<T>({
  columns,
  caption,
  busy = false,
  children,
}: {
  columns: DataColumn<T>[];
  caption: string;
  busy?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="relative overflow-x-auto rounded-lg border bg-card">
      <table className="w-full text-sm" aria-busy={busy || undefined}>
        <caption className="sr-only">{caption}</caption>
        <thead className="border-b bg-muted/50">
          <tr>
            {columns.map((column) => (
              <th
                key={column.id}
                scope="col"
                className={cn(
                  "h-9 px-3 text-left text-xs font-medium whitespace-nowrap text-muted-foreground",
                  column.align === "right" && "text-right",
                )}
              >
                {column.card === "actions" ? <span className="sr-only">{column.header}</span> : column.header}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

function RowCard<T>({ columns, row }: { columns: DataColumn<T>[]; row: T }) {
  const title = columns.find((column) => column.card === "title");
  const actions = columns.filter((column) => column.card === "actions");
  const fields = columns.filter((column) => column.card === undefined);
  return (
    <div className="grid gap-3 rounded-lg border bg-card p-3">
      {title && <div className="min-w-0 font-medium">{title.cell(row)}</div>}
      <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] items-center gap-x-4 gap-y-2 text-sm">
        {fields.map((column) => (
          <div key={column.id} className="contents">
            <dt className="text-muted-foreground">{column.header}</dt>
            <dd className={cn("min-w-0 justify-self-end text-right [&>*]:ml-auto", column.className)}>{column.cell(row)}</dd>
          </div>
        ))}
      </dl>
      {actions.length > 0 && (
        <div className="flex flex-wrap justify-end gap-2 border-t pt-3">
          {actions.map((column) => (
            <div key={column.id}>{column.cell(row)}</div>
          ))}
        </div>
      )}
    </div>
  );
}

function CardSkeleton({ count }: { count: number }) {
  return (
    <div className="grid gap-3" aria-busy="true">
      {Array.from({ length: count }, (_, i) => (
        <div key={i} data-testid="skeleton-card" className="grid gap-2 rounded-lg border bg-card p-3">
          <Skeleton className="h-4 w-1/2" />
          <Skeleton className="h-4 w-full" />
          <Skeleton className="h-4 w-3/4" />
        </div>
      ))}
    </div>
  );
}
