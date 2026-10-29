import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/utils";

/** A toggle chip (filters, presets, segmented choices); `pressed` is announced as aria-pressed. */
export function Chip({ pressed, className, ...props }: ComponentProps<"button"> & { pressed: boolean }) {
  return (
    <button
      type="button"
      aria-pressed={pressed}
      className={cn(
        "inline-flex h-7 items-center rounded-full border px-3 text-xs font-medium transition-colors outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11 max-md:px-4",
        pressed
          ? "border-primary bg-primary text-primary-foreground"
          : "border-input bg-card text-foreground hover:border-foreground/30 hover:bg-accent",
        className,
      )}
      {...props}
    />
  );
}
