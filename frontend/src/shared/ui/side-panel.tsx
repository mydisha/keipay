import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { Sheet, SheetContent, SheetDescription, SheetTitle } from "./sheet";

interface SidePanelProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: ReactNode;
  description?: ReactNode;
  children: ReactNode;
  /** Pinned below the scrolling body, e.g. a form's buttons. */
  footer?: ReactNode;
  /** While true, Escape, outside clicks and the close button do nothing (a request is in flight). */
  locked?: boolean;
  className?: string;
}

/**
 * A detail or form panel that slides in from the right: full width on phones,
 * 36rem from 640px. The body scrolls; the header and footer stay put.
 */
export function SidePanel({ open, onOpenChange, title, description, children, footer, locked = false, className }: SidePanelProps) {
  return (
    <Sheet open={open} onOpenChange={(next) => !locked && onOpenChange(next)}>
      <SheetContent
        side="right"
        className={cn("w-full max-w-full gap-0 bg-background text-foreground sm:max-w-xl", className)}
        onEscapeKeyDown={(event) => locked && event.preventDefault()}
        onInteractOutside={(event) => locked && event.preventDefault()}
      >
        <div className="grid gap-1 border-b px-4 py-3 pr-14 md:px-6">
          <SheetTitle className="text-base font-semibold">{title}</SheetTitle>
          {description ? (
            <SheetDescription className="text-sm text-muted-foreground">{description}</SheetDescription>
          ) : (
            <SheetDescription className="sr-only">{title}</SheetDescription>
          )}
        </div>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4 md:px-6">{children}</div>
        {footer && <div className="flex flex-wrap justify-end gap-2 border-t px-4 py-3 md:px-6">{footer}</div>}
      </SheetContent>
    </Sheet>
  );
}
