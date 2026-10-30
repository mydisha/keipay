import type { ComponentProps, ReactNode } from "react";
import { cn } from "@/shared/lib/utils";

type HeadingLevel = 2 | 3;

interface SectionHeaderProps {
  /** The heading's id; the section it names refers to it with `aria-labelledby`. */
  id: string;
  title: string;
  description?: string;
  actions?: ReactNode;
  headingLevel?: HeadingLevel;
  className?: string;
}

/** A section's heading, one line of context, and the actions that belong to it. */
export function SectionHeader({ id, title, description, actions, headingLevel = 2, className }: SectionHeaderProps) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <div className={cn("flex flex-wrap items-end justify-between gap-3", className)}>
      <div className="min-w-0 space-y-0.5">
        <Heading id={id} className={headingLevel === 2 ? "text-[0.9375rem] font-medium leading-[1.375rem]" : "text-sm font-medium"}>
          {title}
        </Heading>
        {description && <p className="text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}

type SectionProps = Omit<SectionHeaderProps, "className"> & Omit<ComponentProps<"section">, "id" | "title">;

/** A labelled region: `SectionHeader` and its content, stacked with the standard gap. */
export function Section({ id, title, description, actions, headingLevel, className, children, ...props }: SectionProps) {
  return (
    <section aria-labelledby={id} className={cn("grid gap-3", className)} {...props}>
      <SectionHeader id={id} title={title} description={description} actions={actions} headingLevel={headingLevel} />
      {children}
    </section>
  );
}
