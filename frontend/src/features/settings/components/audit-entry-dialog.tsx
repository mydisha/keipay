import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { IdText } from "@/shared/ui/id-text";
import { JsonViewer } from "@/shared/ui/json-viewer";
import type { AuditLog } from "../api/audit-logs";

export function AuditEntryDialog({ entry, onClose }: { entry: AuditLog | null; onClose: () => void }) {
  return (
    <Dialog open={entry !== null} onOpenChange={(open) => !open && onClose()}>
      {entry && (
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle className="font-mono">{entry.action}</DialogTitle>
            <DialogDescription>{strings.settings.entryTitle}</DialogDescription>
          </DialogHeader>
          <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-4 gap-y-2 text-sm">
            <Fact label={strings.settings.entryId}>
              <IdText value={entry.id} label={strings.settings.entryTitle} />
            </Fact>
            <Fact label={strings.settings.time}>
              <DateTimeText value={entry.occurred_at} />
            </Fact>
            <Fact label={strings.settings.actor}>
              <span className="font-mono text-xs">
                {entry.actor.type}:{entry.actor.id}
              </span>
            </Fact>
            <Fact label={strings.settings.target}>
              <span className="font-mono text-xs break-all">
                {entry.target.type}:{entry.target.id}
              </span>
            </Fact>
            <Fact label={strings.settings.ip}>{entry.ip || strings.common.none}</Fact>
            <Fact label={strings.settings.userAgent}>
              <span className="break-all">{entry.user_agent || strings.common.none}</span>
            </Fact>
            <Fact label={strings.settings.requestId}>
              {entry.request_id ? <IdText value={entry.request_id} label={strings.settings.requestId} /> : strings.common.none}
            </Fact>
          </dl>
          <div className="grid gap-2">
            <JsonViewer label={strings.settings.before} value={entry.before} defaultOpen />
            <JsonViewer label={strings.settings.after} value={entry.after} defaultOpen />
          </div>
        </DialogContent>
      )}
    </Dialog>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </>
  );
}
