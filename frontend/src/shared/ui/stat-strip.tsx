import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { Skeleton } from "./skeleton";

export type StatTone = "positive" | "negative" | "neutral";

export interface StatChange {
  /** Says what it is compared with ("+12,5%"; the page header names the period it is compared with); the colour only reinforces the text. */
  text: string;
  tone?: StatTone;
}

const toneClasses: Record<StatTone, string> = {
  positive: "text-success-foreground",
  negative: "text-danger-foreground",
  neutral: "text-muted-foreground",
};

/**
 * Key figures that belong together, as one surface split by hairlines rather than separate
 * cards. Two across on a phone, four from 1024px.
 */
export function StatStrip({ className, children, ...props }: { className?: string; children: ReactNode } & { "aria-label"?: string }) {
  return (
    <dl className={cn("grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border shadow-xs lg:grid-cols-4", className)} {...props}>
      {children}
    </dl>
  );
}

interface StatProps {
  label: string;
  value: ReactNode;
  change?: StatChange;
  /** One line of context under the value, such as "3 of 4 finished". */
  hint?: string;
  /** The value is a word for missing data ("Not yet"), set quieter so it never reads as a figure. */
  empty?: boolean;
}

/** One figure of a `StatStrip`: the label, the value in tabular figures, then what it is compared with. */
export function Stat({ label, value, change, hint, empty = false }: StatProps) {
  return (
    <div className="grid content-start gap-1 bg-card p-4 md:p-5">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="grid gap-1">
        <span
          className={cn(
            "text-2xl leading-8 font-medium tracking-[-0.02em] tabular-nums md:text-[1.75rem]",
            empty && "text-muted-foreground md:text-2xl",
          )}
        >
          {value}
        </span>
        {change && <span className={cn("text-xs", toneClasses[change.tone ?? "neutral"])}>{change.text}</span>}
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      </dd>
    </div>
  );
}

/** A strip's shape while its figures load. */
export function StatStripSkeleton({ count = 4, className }: { count?: number; className?: string }) {
  return (
    <div
      aria-busy="true"
      data-testid="stat-strip-skeleton"
      className={cn("grid grid-cols-2 gap-px overflow-hidden rounded-xl border bg-border lg:grid-cols-4", className)}
    >
      {Array.from({ length: count }, (_, i) => (
        <div key={i} className="grid gap-2 bg-card p-4 md:p-5">
          <Skeleton className="h-4 w-20" />
          <Skeleton className="h-7 w-28" />
          <Skeleton className="h-3 w-32" />
        </div>
      ))}
    </div>
  );
}
