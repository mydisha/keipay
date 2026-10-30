import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

interface BrandProps {
  /** Shows only the initial (collapsed sidebar); the full name stays readable for screen readers. */
  compact?: boolean;
  className?: string;
}

/** The product name as a text wordmark. KeiPay has no drawn logo yet. */
export function Brand({ compact = false, className }: BrandProps) {
  return (
    <span className={cn("text-base font-semibold tracking-[-0.02em]", className)}>
      {compact ? (
        <>
          <span aria-hidden="true">K</span>
          <span className="sr-only">{strings.product}</span>
        </>
      ) : (
        strings.product
      )}
    </span>
  );
}
