import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { summarizeSubscriptions } from "../lib/endpoint-format";

const chip = "inline-flex items-center rounded-sm border px-1.5 py-px text-xs font-medium whitespace-nowrap";

/** A neutral text badge: body version, Legacy signature, Migrated, Test. */
export function TagBadge({ children, mono = false, title }: { children: string; mono?: boolean; title?: string }) {
  return (
    <span title={title} className={cn(chip, "border-neutral-border bg-neutral text-neutral-foreground", mono && "font-mono")}>
      {children}
    </span>
  );
}

/** Up to three subscriptions in words, then "+N". */
export function SubscriptionSummary({ events }: { events: string[] }) {
  const { shown, more } = summarizeSubscriptions(events);
  return (
    <span className="flex flex-wrap items-center gap-1" title={events.join(", ")}>
      {shown.map((label) => (
        <span key={label} className={cn("text-[0.8125rem]", label.includes(".") && "font-mono")}>
          {label}
          {(label !== shown[shown.length - 1] || more > 0) && ","}
        </span>
      ))}
      {more > 0 && <span className="text-xs text-muted-foreground">{strings.endpoints.moreEvents(more)}</span>}
    </span>
  );
}
