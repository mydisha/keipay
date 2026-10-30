import { strings } from "@/shared/lib/strings";
import { StatusBadge, type StatusTone } from "@/shared/ui/status-badge";
import type { ReconciliationItem, ReconciliationResult, RunStatus } from "../api/runs";

const RUN_TONES: Record<RunStatus, StatusTone> = {
  queued: "neutral",
  running: "info",
  succeeded: "success",
  failed: "danger",
};

/** A run's status as a text stamp. */
export function RunStatusBadge({ status }: { status: RunStatus }) {
  return <StatusBadge status={status} label={strings.reconciliation.runStatus[status] ?? status} tone={RUN_TONES[status]} />;
}

const RESULT_TONES: Record<ReconciliationResult, StatusTone> = {
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
