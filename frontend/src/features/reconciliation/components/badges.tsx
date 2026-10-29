import { CircleCheckIcon, CircleXIcon, ClockIcon, Loader2Icon, type LucideIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { StatusBadge, toneClasses, type StatusTone } from "@/shared/ui/status-badge";
import type { ReconciliationItem, ReconciliationResult, RunStatus } from "../api/runs";

const RUN_STATUS: Record<RunStatus, { tone: StatusTone; icon: LucideIcon; spin?: boolean }> = {
  queued: { tone: "neutral", icon: ClockIcon },
  running: { tone: "info", icon: Loader2Icon, spin: true },
  succeeded: { tone: "success", icon: CircleCheckIcon },
  failed: { tone: "danger", icon: CircleXIcon },
};

/** A run's status as text with an icon. */
export function RunStatusBadge({ status }: { status: RunStatus }) {
  const { tone, icon: Icon, spin } = RUN_STATUS[status];
  return (
    <span
      data-tone={tone}
      className={cn(
        "inline-flex items-center gap-1.5 rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        toneClasses[tone],
      )}
    >
      <Icon aria-hidden="true" className={cn("size-3.5", spin && "animate-spin motion-reduce:animate-none")} />
      {strings.reconciliation.runStatus[status] ?? status}
    </span>
  );
}

export const RESULT_TONES: Record<ReconciliationResult, StatusTone> = {
  matched: "success",
  missing_in_keipay: "danger",
  missing_in_gateway: "danger",
  amount_mismatch: "danger",
  status_mismatch: "warning",
  fee_mismatch: "warning",
};

export function resultLabel(result: ReconciliationResult): string {
  return strings.reconciliation.results[result] ?? result;
}

export function ResultBadge({ result }: { result: ReconciliationResult }) {
  return <StatusBadge status={result} label={resultLabel(result)} tone={RESULT_TONES[result]} />;
}

/** "Resolved" or "Unresolved"; matched items need neither. */
export function ResolutionBadge({ item }: { item: ReconciliationItem }) {
  if (item.result === "matched") return <span className="text-muted-foreground">{strings.common.none}</span>;
  return item.resolution ? (
    <StatusBadge status="resolved" label={strings.reconciliation.resolved} tone="info" />
  ) : (
    <StatusBadge status="unresolved" label={strings.reconciliation.open} tone="neutral" />
  );
}
