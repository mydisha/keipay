import { cn } from "@/shared/lib/utils";
import { strings } from "@/shared/lib/strings";

export type StatusTone = "success" | "warning" | "danger" | "info" | "neutral";

const TONES: Record<string, StatusTone> = {
  PENDING: "warning",
  PAID: "success",
  SETTLED: "info",
  EXPIRED: "neutral",
  FAILED: "danger",
  DELIVERED: "success",
  active: "success",
  inactive: "neutral",
};

export const toneClasses: Record<StatusTone, string> = {
  success: "border-success-border bg-success text-success-foreground",
  warning: "border-warning-border bg-warning text-warning-foreground",
  danger: "border-danger-border bg-danger text-danger-foreground",
  info: "border-info-border bg-info text-info-foreground",
  neutral: "border-neutral-border bg-neutral text-neutral-foreground",
};

export function statusTone(status: string): StatusTone {
  return TONES[status] ?? "neutral";
}

/** Status as a text label; colour only reinforces it. */
export function StatusBadge({ status, className }: { status: string; className?: string }) {
  const tone = statusTone(status);
  const label = strings.status[status] ?? status;
  return (
    <span
      data-tone={tone}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        toneClasses[tone],
        className,
      )}
    >
      <span aria-hidden="true" className="size-1.5 rounded-full bg-current" />
      {label}
    </span>
  );
}
