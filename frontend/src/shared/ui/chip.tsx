import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/utils";

/**
 * A toggle chip (filters, presets, segmented choices); `pressed` is announced as aria-pressed.
 * Off is a dashed outline, on is the pine signal, so the active filter reads at a glance.
 */
export function Chip({ pressed, className, ...props }: ComponentProps<"button"> & { pressed: boolean }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className={cn(
        "inline-flex h-7 items-center rounded-sm border px-2.5 text-xs font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11 max-md:px-4",
        pressed
          ? "border-brand bg-brand-soft text-brand"
          : "border-dashed border-input text-muted-foreground hover:border-foreground/50 hover:text-foreground",
        className,
      )}
      {...props}
    />
  );
}
