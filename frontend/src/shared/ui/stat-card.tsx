import { ArrowDownRightIcon, ArrowUpRightIcon, MinusIcon } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { Card, CardContent } from "./card";
import { Skeleton } from "./skeleton";

export type DeltaDirection = "up" | "down" | "flat";
export type DeltaTone = "positive" | "negative" | "neutral";

export interface StatDelta {
  direction: DeltaDirection;
  /** Always text ("+12% vs previous 7 days"); the arrow and colour only reinforce it. */
  text: string;
  tone?: DeltaTone;
}

interface StatCardProps {
  label: string;
  value: ReactNode;
  delta?: StatDelta;
  /** Secondary text under the value, such as the unit or a note. */
  hint?: string;
  sparkline?: ReactNode;
  isLoading?: boolean;
  className?: string;
}

const deltaIcons: Record<DeltaDirection, typeof ArrowUpRightIcon> = {
  up: ArrowUpRightIcon,
  down: ArrowDownRightIcon,
  flat: MinusIcon,
};

const deltaTones: Record<DeltaTone, string> = {
  positive: "text-success-foreground",
  negative: "text-danger-foreground",
  neutral: "text-muted-foreground",
};

/** A key figure: label, big tabular value, an optional change versus the previous period and a sparkline. */
export function StatCard({ label, value, delta, hint, sparkline, isLoading = false, className }: StatCardProps) {
  if (isLoading) return <StatCardSkeleton className={className} />;
  const DeltaIcon = delta ? deltaIcons[delta.direction] : null;
  return (
    <Card size="compact" className={className}>
      <CardContent className="grid content-start gap-1">
        <span className="text-xs font-medium text-muted-foreground">{label}</span>
        <span className="text-2xl font-semibold tracking-tight tabular-nums">{value}</span>
        {delta && DeltaIcon && (
          <span className={cn("flex items-center gap-1 text-xs", deltaTones[delta.tone ?? "neutral"])}>
            <DeltaIcon className="size-3.5 shrink-0" aria-hidden="true" />
            {delta.text}
          </span>
        )}
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
        {sparkline && <div className="mt-2">{sparkline}</div>}
      </CardContent>
    </Card>
  );
}

export function StatCardSkeleton({ className }: { className?: string }) {
  return (
    <Card size="compact" aria-busy="true" data-testid="stat-card-skeleton" className={className}>
      <CardContent className="grid gap-2">
        <Skeleton className="h-3 w-20" />
        <Skeleton className="h-7 w-28" />
        <Skeleton className="h-3 w-32" />
      </CardContent>
    </Card>
  );
}
