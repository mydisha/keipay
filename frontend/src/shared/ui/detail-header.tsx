import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { BackLink } from "./back-link";
import { Skeleton } from "./skeleton";

interface DetailHeaderProps {
  back: { to: string; label: string };
  /** A short label above the title, such as the resource type. */
  eyebrow?: string;
  /** The resource's name or id, rendered as the page `h1`. */
  title: string;
  titleMono?: boolean;
  /** Badges and other text next to the title (environment, status, a copy button). */
  badges?: ReactNode;
  description?: string;
  /** Key facts under the title, usually a `FactList`. */
  facts?: ReactNode;
  actions?: ReactNode;
}

/** The header of a resource detail page: back link, title with its badges, facts and actions. */
export function DetailHeader({ back, eyebrow, title, titleMono = false, badges, description, facts, actions }: DetailHeaderProps) {
  return (
    <div className="grid gap-4">
      <BackLink to={back.to} label={back.label} />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-2">
          {eyebrow && <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{eyebrow}</p>}
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h1
              className={cn("min-w-0 truncate text-xl font-semibold tracking-tight md:text-2xl", titleMono && "font-mono")}
              title={title}
            >
              {title}
            </h1>
            {badges}
          </div>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </div>
      {facts}
    </div>
  );
}

/** The header's shape while the resource loads. */
export function DetailHeaderSkeleton({ testId }: { testId: string }) {
  return (
    <div className="grid gap-4" aria-busy="true" data-testid={testId}>
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-8 w-64 max-w-full" />
      <Skeleton className="h-4 w-80 max-w-full" />
    </div>
  );
}
