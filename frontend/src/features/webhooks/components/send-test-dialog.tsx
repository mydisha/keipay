import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useId, useState } from "react";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Label } from "@/shared/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import type { WebhookEndpoint } from "../api/endpoints";
import { useEventTypes, type EventType } from "../api/event-types";

interface SendTestDialogProps {
  endpoint: WebhookEndpoint | null;
  pending: boolean;
  onClose: () => void;
  onSend: (endpoint: WebhookEndpoint, eventType: EventType) => void;
}

/** Picks a catalog type (never a wildcard) for a one-off test send. */
export function SendTestDialog({ endpoint, pending, onClose, onSend }: SendTestDialogProps) {
  return (
    <Dialog open={endpoint !== null} onOpenChange={(open) => !open && !pending && onClose()}>
      {endpoint && <SendTestBody endpoint={endpoint} pending={pending} onClose={onClose} onSend={onSend} />}
    </Dialog>
  );
}

function SendTestBody({ endpoint, pending, onClose, onSend }: SendTestDialogProps & { endpoint: WebhookEndpoint }) {
  const catalog = useEventTypes();
  const [chosen, setChosen] = useState<EventType | null>(null);
  const selectId = useId();
  const eventType = chosen ?? catalog.data?.data[0]?.type ?? null;

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{strings.endpoints.sendTestTitle}</DialogTitle>
        <DialogDescription>{strings.endpoints.sendTestBody}</DialogDescription>
      </DialogHeader>
      <form
        className="grid gap-4"
        onSubmit={(event) => {
          event.preventDefault();
          if (eventType && !pending) onSend(endpoint, eventType);
        }}
      >
        <div className="grid gap-2">
          <Label htmlFor={selectId}>{strings.endpoints.sendTestEventType}</Label>
          {catalog.isPending ? (
            <Skeleton className="h-9 w-full" />
          ) : catalog.isError ? (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{strings.endpoints.catalogFailed}</AlertDescription>
            </Alert>
          ) : (
            <Select value={eventType ?? undefined} onValueChange={(value) => setChosen(value as EventType)} disabled={pending}>
              <SelectTrigger id={selectId} className="font-mono">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {catalog.data.data.map((info) => (
                  <SelectItem key={info.type} value={info.type} description={info.description}>
                    <span className="font-mono">{info.type}</span>
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={pending}>
            {strings.common.cancel}
          </Button>
          <Button type="submit" disabled={pending || !eventType}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.endpoints.sending : strings.endpoints.sendTest}
          </Button>
        </DialogFooter>
      </form>
    </DialogContent>
  );
}
