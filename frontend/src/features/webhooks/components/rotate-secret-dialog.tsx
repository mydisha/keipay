import { useId, useState } from "react";
import { strings } from "@/shared/lib/strings";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import type { WebhookEndpoint } from "../api/endpoints";
import { urlHost } from "../lib/endpoint-format";

export const DEFAULT_OVERLAP_HOURS = 24;
const MAX_OVERLAP_HOURS = 168;

/** A whole number of hours from 0 to 168, or null. */
export function parseOverlapHours(text: string): number | null {
  if (!/^\d+$/.test(text.trim())) return null;
  const hours = Number(text);
  return hours <= MAX_OVERLAP_HOURS ? hours : null;
}

interface RotateSecretDialogProps {
  endpoint: WebhookEndpoint | null;
  pending: boolean;
  onClose: () => void;
  onConfirm: (endpoint: WebhookEndpoint, overlapHours: number) => void;
}

/** Asks for the overlap and the URL host before issuing a new signing secret. */
export function RotateSecretDialog({ endpoint, pending, onClose, onConfirm }: RotateSecretDialogProps) {
  return (
    <RotateSecretBody key={endpoint?.id ?? "none"} endpoint={endpoint} pending={pending} onClose={onClose} onConfirm={onConfirm} />
  );
}

function RotateSecretBody({ endpoint, pending, onClose, onConfirm }: RotateSecretDialogProps) {
  const [overlap, setOverlap] = useState(String(DEFAULT_OVERLAP_HOURS));
  const inputId = useId();
  const hours = parseOverlapHours(overlap);
  const invalid = hours === null;

  return (
    <ConfirmDialog
      open={endpoint !== null}
      onOpenChange={(open) => !open && onClose()}
      title={strings.endpoints.rotateTitle}
      description={strings.endpoints.rotateBody}
      confirmText={endpoint ? urlHost(endpoint.url) : undefined}
      confirmLabel={strings.endpoints.rotateConfirm}
      pendingLabel={strings.endpoints.rotating}
      pending={pending}
      destructive
      canConfirm={!invalid}
      onConfirm={() => endpoint && hours !== null && onConfirm(endpoint, hours)}
    >
      <div className="grid gap-2">
        <Label htmlFor={inputId}>{strings.endpoints.overlapHours}</Label>
        <Input
          id={inputId}
          inputMode="numeric"
          autoComplete="off"
          className="tabular-nums"
          value={overlap}
          onChange={(event) => setOverlap(event.target.value)}
          aria-invalid={invalid}
          aria-describedby={`${inputId}-hint`}
          disabled={pending}
        />
        <p id={`${inputId}-hint`} className={invalid ? "text-xs font-medium text-danger-foreground" : "text-xs text-muted-foreground"}>
          {invalid ? strings.endpoints.overlapInvalid : strings.endpoints.overlapHint}
        </p>
      </div>
    </ConfirmDialog>
  );
}
