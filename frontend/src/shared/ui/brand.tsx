import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

interface BrandProps {
  /** Hides the product name visually (collapsed sidebar). */
  compact?: boolean;
  /** `onPanel` renders the mark for the dark brand panel of the auth pages. */
  tone?: "default" | "onPanel";
  className?: string;
}

/** The product mark (a K whose strokes end in route nodes) and name. */
export function Brand({ compact = false, tone = "default", className }: BrandProps) {
  return (
    <span className={cn("flex items-center gap-2 font-semibold", className)}>
      <BrandMark className={tone === "onPanel" ? "bg-brand-panel-foreground text-brand-panel" : "bg-primary text-primary-foreground"} />
      <span className={cn(compact && "sr-only")}>{strings.product}</span>
    </span>
  );
}

function BrandMark({ className }: { className: string }) {
  return (
    <span aria-hidden="true" className={cn("flex size-7 shrink-0 items-center justify-center rounded-md", className)}>
      <svg viewBox="0 0 24 24" className="size-4" fill="none" stroke="currentColor" strokeWidth={2.4} strokeLinecap="round" strokeLinejoin="round">
        <path d="M7 5v14" />
        <path d="M7 12l9-7" />
        <path d="M7 12l9 7" />
        <circle cx="7" cy="5" r="1.6" fill="currentColor" stroke="none" />
        <circle cx="7" cy="19" r="1.6" fill="currentColor" stroke="none" />
        <circle cx="16" cy="5" r="1.6" fill="currentColor" stroke="none" />
        <circle cx="16" cy="19" r="1.6" fill="currentColor" stroke="none" />
      </svg>
    </span>
  );
}
