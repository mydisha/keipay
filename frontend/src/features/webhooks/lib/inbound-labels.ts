import { strings } from "@/shared/lib/strings";
import type { StatusTone } from "@/shared/ui/status-badge";
import type { InboundResult } from "../api/inbound";

const RESULT_TONES: Record<InboundResult, StatusTone> = {
  applied: "success",
  stale: "neutral",
  duplicate: "neutral",
  mismatch: "danger",
  unmapped: "warning",
  invalid_signature: "danger",
  invalid_payload: "danger",
  error: "danger",
};

export function resultLabel(result: string): string {
  return strings.webhooks.results[result] ?? result;
}

export function resultTone(result: string): StatusTone {
  return RESULT_TONES[result as InboundResult] ?? "neutral";
}

export function signatureLabel(valid: boolean): string {
  return valid ? strings.webhooks.signatureLabels.valid : strings.webhooks.signatureLabels.invalid;
}

/** Pretty JSON when the raw body parses, else null (shown as text). */
export function parseJsonBody(raw: string): { value: unknown } | null {
  try {
    return { value: JSON.parse(raw) as unknown };
  } catch {
    return null;
  }
}
