import { createContext, useContext, type ComponentProps, type ReactNode } from "react";
import { cn } from "@/shared/lib/utils";

type FactLayout = "rows" | "inline" | "tiles";
type LabelWidth = "sm" | "md";

interface FactListProps extends ComponentProps<"dl"> {
  /** `rows`: label beside value; `inline`: short pairs that wrap; `tiles`: a grid of small label-over-value cells. */
  layout?: FactLayout;
  /** Label column of the `rows` layout: 9rem or 12rem. */
  labelWidth?: LabelWidth;
}

const FactContext = createContext<{ layout: FactLayout; labelWidth: LabelWidth }>({ layout: "rows", labelWidth: "md" });

const listClasses: Record<FactLayout, string> = {
  rows: "grid gap-3 text-sm",
  inline: "flex min-w-0 flex-wrap items-center gap-x-6 gap-y-1 text-sm",
  tiles: "grid grid-cols-2 gap-x-6 gap-y-3 md:grid-cols-4",
};

/** A definition list of label/value facts. */
export function FactList({ layout = "rows", labelWidth = "md", className, ...props }: FactListProps) {
  return (
    <FactContext.Provider value={{ layout, labelWidth }}>
      <dl className={cn(listClasses[layout], className)} {...props} />
    </FactContext.Provider>
  );
}

interface FactProps {
  label: string;
  children: ReactNode;
  /** Renders the value in the mono face (slugs, ids, keys). */
  mono?: boolean;
  className?: string;
}

/** One label/value pair of a `FactList`; always `<div><dt/><dd/></div>`. */
export function Fact({ label, children, mono = false, className }: FactProps) {
  const { layout, labelWidth } = useContext(FactContext);
  const valueClass = mono ? "font-mono text-[0.8125rem]" : undefined;
  if (layout === "inline") {
    return (
      <div className={cn("flex min-w-0 items-center gap-2", className)}>
        <dt className="text-muted-foreground">{label}</dt>
        <dd className={cn("min-w-0 truncate", valueClass)}>{children}</dd>
      </div>
    );
  }
  if (layout === "tiles") {
    return (
      <div className={cn("grid min-w-0 gap-1", className)}>
        <dt className="text-xs text-muted-foreground">{label}</dt>
        <dd className={cn("min-w-0 text-sm", valueClass)}>{children}</dd>
      </div>
    );
  }
  return (
    <div
      className={cn(
        "grid min-w-0 gap-1 sm:gap-4",
        labelWidth === "sm" ? "sm:grid-cols-[9rem_minmax(0,1fr)]" : "sm:grid-cols-[12rem_minmax(0,1fr)]",
        className,
      )}
    >
      <dt className="text-muted-foreground">{label}</dt>
      <dd className={cn("min-w-0", valueClass)}>{children}</dd>
    </div>
  );
}
