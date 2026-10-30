import { strings } from "@/shared/lib/strings";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatCardSkeleton } from "@/shared/ui/stat-card";

/** The dashboard's shape while it loads: four figures, the chart beside two cards, and the table. */
export function DashboardSkeleton() {
  return (
    <div className="grid gap-6" aria-busy="true" data-testid="dashboard-skeleton">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {Array.from({ length: 4 }, (_, i) => (
          <StatCardSkeleton key={i} />
        ))}
      </div>
      <div className="grid gap-4 xl:grid-cols-[2fr_1fr]">
        <Skeleton className="h-80 w-full" />
        <div className="grid gap-4">
          <Skeleton className="h-40 w-full" />
          <Skeleton className="h-36 w-full" />
        </div>
      </div>
      <Skeleton className="h-56 w-full" />
    </div>
  );
}
