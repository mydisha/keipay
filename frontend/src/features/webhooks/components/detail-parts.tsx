import { useId, type ReactNode } from "react";
import { Fact as SharedFact, FactList } from "@/shared/ui/fact-list";
import { Section } from "@/shared/ui/section";
import { Skeleton } from "@/shared/ui/skeleton";

/** A label/value row in a detail panel. */
export function Fact({ label, children, className }: { label: string; children: ReactNode; className?: string }) {
  return (
    <SharedFact label={label} className={className}>
      {children}
    </SharedFact>
  );
}

/** The `dl` that holds a panel's facts, with the narrow label column of a side panel. */
export function Facts({ children, className }: { children: ReactNode; className?: string }) {
  return (
    <FactList labelWidth="sm" className={className}>
      {children}
    </FactList>
  );
}

export function DetailSection({ title, children }: { title: string; children: ReactNode }) {
  const id = useId();
  return (
    <Section id={id} title={title} headingLevel={3} className="gap-2">
      {children}
    </Section>
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
