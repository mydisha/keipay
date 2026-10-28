import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

export function Brand({ compact = false, className }: { compact?: boolean; className?: string }) {
  return (
    <span className={cn("flex items-center gap-2 font-semibold", className)}>
      <span
        aria-hidden="true"
        className="flex size-7 items-center justify-center rounded-md bg-primary text-xs text-primary-foreground"
      >
        K
      </span>
      <span className={cn(compact && "sr-only")}>{strings.product}</span>
    </span>
  );
}
