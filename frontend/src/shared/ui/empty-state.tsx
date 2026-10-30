import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";

interface EmptyStateProps {
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
  /** Heading level for the title; pages use h2, nested sections h3. */
  headingLevel?: 2 | 3;
  /** `inline` drops the surface, for an empty state inside a card or panel that already has one. */
  variant?: "surface" | "inline";
}

/** What will appear here, why it is empty, and the one action that fills it. */
export function EmptyState({ title, description, action, className, headingLevel = 2, variant = "surface" }: EmptyStateProps) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <div
      data-testid="empty-state"
      className={cn(
        "grid justify-items-start gap-3",
        variant === "surface" && "rounded-xl border bg-card px-5 py-8 shadow-xs",
        className,
      )}
    >
      <div className="grid max-w-prose gap-1">
        <Heading className="text-[0.9375rem] font-medium">{title}</Heading>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {action && <div className="flex flex-wrap gap-2">{action}</div>}
    </div>
  );
}
