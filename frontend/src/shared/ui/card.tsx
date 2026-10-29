import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/utils";

type CardSize = "default" | "compact";

// The parts read their padding from these variables, so a part can still opt
// out with `className="p-0"` (a table flush to the card edge).
const sizeVars: Record<CardSize, string> = {
  default: "[--card-pad:1rem] [--card-pad-y:0.75rem] md:[--card-pad:1.25rem] md:[--card-pad-y:1rem]",
  compact: "[--card-pad:1rem] [--card-pad-y:1rem]",
};

/** A bordered surface. Build it from `CardHeader`, `CardContent` and `CardFooter`; never pad the card itself. */
export function Card({ className, size = "default", ...props }: ComponentProps<"div"> & { size?: CardSize }) {
  return (
    <div
      data-slot="card"
      data-size={size}
      className={cn("group/card rounded-lg border bg-card text-card-foreground shadow-xs", sizeVars[size], className)}
      {...props}
    />
  );
}

/** Title and description, with an optional `CardAction` on the right. */
export function CardHeader({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-header"
      className={cn(
        "grid auto-rows-min items-start gap-1 border-b px-(--card-pad) py-(--card-pad-y) has-data-[slot=card-action]:grid-cols-[1fr_auto]",
        className,
      )}
      {...props}
    />
  );
}

export function CardTitle({ className, as: Heading = "h2", ...props }: ComponentProps<"h2"> & { as?: "h2" | "h3" }) {
  return (
    <Heading
      data-slot="card-title"
      className={cn(Heading === "h2" ? "text-base font-semibold leading-6" : "text-sm font-semibold leading-5", className)}
      {...props}
    />
  );
}

export function CardDescription({ className, ...props }: ComponentProps<"p">) {
  return <p data-slot="card-description" className={cn("text-sm text-muted-foreground", className)} {...props} />;
}

export function CardAction({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-action"
      className={cn("col-start-2 row-span-2 row-start-1 flex flex-wrap gap-2 self-start justify-self-end", className)}
      {...props}
    />
  );
}

export function CardContent({ className, ...props }: ComponentProps<"div">) {
  return <div data-slot="card-content" className={cn("p-(--card-pad)", className)} {...props} />;
}

export function CardFooter({ className, ...props }: ComponentProps<"div">) {
  return (
    <div
      data-slot="card-footer"
      className={cn("flex flex-wrap items-center gap-3 border-t px-(--card-pad) py-(--card-pad-y)", className)}
      {...props}
    />
  );
}
