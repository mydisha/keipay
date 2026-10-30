import { strings } from "@/shared/lib/strings";
import { toneClasses } from "@/shared/ui/status-badge";
import { cn } from "@/shared/lib/utils";

/** Marks the connection that `POST /webhooks/xendit` (the pre-connections callback URL) also reaches. */
export function LegacyUrlBadge({ className }: { className?: string }) {
  return (
    <span
      className={cn(
        "inline-flex items-center rounded-sm border px-1.5 py-px text-xs font-medium whitespace-nowrap",
        toneClasses.info,
        className,
      )}
    >
      {strings.connections.legacyUrl}
    </span>
  );
}
