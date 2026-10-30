import { strings } from "@/shared/lib/strings";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatStripSkeleton } from "@/shared/ui/stat-strip";

/** The dashboard's shape while it loads: the figures over the chart, two ruled lists, and the table. */
export function DashboardSkeleton() {
  return (
    <div className="grid gap-8" aria-busy="true" data-testid="dashboard-skeleton">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="overflow-hidden rounded-xl border bg-card">
        <StatStripSkeleton className="rounded-none border-0 border-b" />
        <div className="p-4 md:p-5">
          <Skeleton className="h-64 w-full" />
        </div>
      </div>
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
        <Skeleton className="h-44 w-full rounded-xl" />
        <Skeleton className="h-44 w-full rounded-xl" />
      </div>
      <Skeleton className="h-56 w-full rounded-xl" />
    </div>
  );
}
