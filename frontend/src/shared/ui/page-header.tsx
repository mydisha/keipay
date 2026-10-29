import type { ReactNode } from "react";

interface PageHeaderProps {
  title: string;
  description?: string;
  /** A short label above the title, such as the parent resource. */
  eyebrow?: string;
  actions?: ReactNode;
  /** Controls under the header, such as chips or tabs. */
  children?: ReactNode;
}

/** The page's `h1`, one sentence of context, and its actions. */
export function PageHeader({ title, description, eyebrow, actions, children }: PageHeaderProps) {
  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          {eyebrow && <p className="text-xs font-medium tracking-wide text-muted-foreground uppercase">{eyebrow}</p>}
          <h1 className="text-xl font-semibold tracking-tight md:text-2xl">{title}</h1>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
      </div>
      {children}
    </div>
  );
}
