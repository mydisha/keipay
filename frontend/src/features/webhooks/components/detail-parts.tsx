import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { Skeleton } from "@/shared/ui/skeleton";

/** A label/value row in a detail panel. */
export function Fact({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <div className={cn("grid min-w-0 gap-1 sm:grid-cols-[9rem_minmax(0,1fr)] sm:gap-4", className)}>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

export function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="grid gap-2">
      <h3 className="text-sm font-semibold">{title}</h3>
      {children}
    </section>
  );
}

export function DetailSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true" data-testid="detail-skeleton">
      <Skeleton className="h-6 w-1/2" />
      {Array.from({ length: 5 }, (_, i) => (
        <Skeleton key={i} className="h-4 w-full" />
      ))}
      <Skeleton className="h-24 w-full" />
    </div>
  );
}
