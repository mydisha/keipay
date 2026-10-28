import type { ReactNode } from "react";
import { cn } from "@/shared/lib/utils";
import { DateTimeText } from "./date-time-text";
import type { StatusTone } from "./status-badge";

export interface TimelineItem {
  id: string;
  time: string;
  title: string;
  tone?: StatusTone;
  /** Text label for the tone, so the state is never colour-only. */
  statusLabel?: string;
  body?: ReactNode;
}

const dotClasses: Record<StatusTone, string> = {
  success: "bg-success-foreground",
  warning: "bg-warning-foreground",
  danger: "bg-danger-foreground",
  info: "bg-info-foreground",
  neutral: "bg-neutral-foreground",
};

const labelClasses: Record<StatusTone, string> = {
  success: "text-success-foreground",
  warning: "text-warning-foreground",
  danger: "text-danger-foreground",
  info: "text-info-foreground",
  neutral: "text-neutral-foreground",
};

export function Timeline({ items, now, className }: { items: TimelineItem[]; now?: Date; className?: string }) {
  return (
    <ol className={cn("relative grid gap-4", className)}>
      {items.map((item, index) => {
        const tone = item.tone ?? "neutral";
        return (
          <li key={item.id} className="relative grid grid-cols-[1rem_1fr] gap-3">
            <span className="relative flex justify-center" aria-hidden="true">
              <span className={cn("mt-1.5 size-2.5 rounded-full", dotClasses[tone])} />
              {index < items.length - 1 && <span className="absolute top-5 -bottom-4 w-px bg-border" />}
            </span>
            <div className="min-w-0 space-y-1">
              <div className="flex flex-wrap items-baseline gap-x-2">
                <span className="font-medium">{item.title}</span>
                {item.statusLabel && (
                  <span className={cn("text-xs font-medium", labelClasses[tone])}>{item.statusLabel}</span>
                )}
                <span className="text-xs text-muted-foreground">
                  <DateTimeText value={item.time} now={now} />
                </span>
              </div>
              {item.body && <div className="text-sm text-muted-foreground">{item.body}</div>}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
