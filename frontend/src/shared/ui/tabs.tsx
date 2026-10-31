import { Tabs as TabsPrimitive } from "radix-ui";
import type { ComponentProps } from "react";
import { cn } from "@/shared/lib/utils";

/** One underline tab; the active one shows the pine bar. Shared with `LinkTabs`. */
export const underlineTabClass =
  "relative inline-flex h-10 items-center justify-center rounded-t-md px-3 text-sm font-medium whitespace-nowrap transition-colors outline-none after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:rounded-full after:bg-brand after:opacity-0 after:transition-opacity hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11";

/** Underline tabs; the root stacks the list and the content with the page gap. */
export function Tabs({ className, ...props }: ComponentProps<typeof TabsPrimitive.Root>) {
  return <TabsPrimitive.Root className={cn("grid gap-6", className)} {...props} />;
}

export function TabsList({ className, ...props }: ComponentProps<typeof TabsPrimitive.List>) {
  return (
    <TabsPrimitive.List
      className={cn("flex max-w-full items-end gap-1 overflow-x-auto border-b text-muted-foreground", className)}
      {...props}
    />
  );
}

export function TabsTrigger({ className, ...props }: ComponentProps<typeof TabsPrimitive.Trigger>) {
  return (
    <TabsPrimitive.Trigger
      className={cn(
        underlineTabClass,
        "-mb-px gap-1.5 disabled:pointer-events-none disabled:opacity-50 data-[state=active]:text-foreground data-[state=active]:after:opacity-100",
        className,
      )}
      {...props}
    />
  );
}

export function TabsContent({ className, ...props }: ComponentProps<typeof TabsPrimitive.Content>) {
  return <TabsPrimitive.Content className={cn("outline-none", className)} {...props} />;
}
