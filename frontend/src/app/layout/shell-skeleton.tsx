import { strings } from "@/shared/lib/strings";
import { Skeleton } from "@/shared/ui/skeleton";

/** The console's outline while the signed-in user loads, so nothing flashes. */
export function ShellSkeleton() {
  return (
    <div data-testid="shell-skeleton" aria-busy="true" className="flex min-h-dvh">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="hidden w-64 shrink-0 flex-col border-r border-sidebar-border bg-sidebar md:flex">
        <div className="flex h-14 items-center border-b border-sidebar-border px-4">
          <Skeleton className="h-7 w-28" />
        </div>
        <div className="grid gap-2 px-2 py-3">
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={i} className="h-9 w-full" />
          ))}
        </div>
        <div className="mt-auto px-3 pb-3">
          <Skeleton className="h-8 w-full" />
        </div>
      </div>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex h-14 items-center gap-2 border-b px-4 md:px-6 lg:px-8">
          <Skeleton className="h-9 w-11 sm:w-72" />
          <Skeleton className="ml-auto size-9 rounded-full" />
        </div>
        <div className="mx-auto grid w-full max-w-7xl gap-6 px-4 py-6 md:px-6 md:py-8 lg:px-8">
          <div className="grid gap-2">
            <Skeleton className="h-8 w-40" />
            <Skeleton className="h-4 w-full max-w-md" />
          </div>
          <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
            {Array.from({ length: 4 }, (_, i) => (
              <Skeleton key={i} className="h-28 w-full" />
            ))}
          </div>
          <Skeleton className="h-72 w-full" />
        </div>
      </div>
    </div>
  );
}
