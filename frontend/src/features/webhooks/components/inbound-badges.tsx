import { StatusBadge } from "@/shared/ui/status-badge";
import { resultLabel, resultTone, signatureLabel } from "../lib/inbound-labels";

export function ResultBadge({ result }: { result: string }) {
  return <StatusBadge status={result} label={resultLabel(result)} tone={resultTone(result)} />;
}

export function SignatureBadge({ valid }: { valid: boolean }) {
  return <StatusBadge status={valid ? "verified" : "invalid"} label={signatureLabel(valid)} tone={valid ? "success" : "danger"} />;
}
