import { ScrollTextIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { useInvalidCursorReset } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { useAuditLogs, type AuditLog } from "../api/audit-logs";
import { AuditEntryDialog } from "./audit-entry-dialog";

export function AuditLogSection() {
  const [cursor, setCursor] = useState<string | null>(null);
  const [selected, setSelected] = useState<AuditLog | null>(null);
  const logs = useAuditLogs(cursor);
  const { resetting, resetKey } = useInvalidCursorReset(logs.error, cursor, setCursor, strings.settings.cursorReset);

  const columns: DataColumn<AuditLog>[] = [
    { id: "time", header: strings.settings.time, cell: (e) => <DateTimeText value={e.occurred_at} /> },
    {
      id: "action",
      header: strings.settings.action,
      card: "title",
      cell: (e) => <span className="font-mono text-xs font-medium">{e.action}</span>,
    },
    {
      id: "actor",
      header: strings.settings.actor,
      className: "max-w-56",
      cell: (e) => (
        <span className="block truncate font-mono text-xs" title={`${e.actor.type}:${e.actor.id}`}>
          {e.actor.type}:{e.actor.id}
        </span>
      ),
    },
    {
      id: "target",
      header: strings.settings.target,
      className: "max-w-56",
      cell: (e) => (
        <span className="block truncate font-mono text-xs" title={`${e.target.type}:${e.target.id}`}>
          {e.target.type}:{e.target.id}
        </span>
      ),
    },
    {
      id: "view",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (e) => (
        <Button variant="ghost" size="sm" onClick={() => setSelected(e)} aria-label={strings.settings.viewEntry(e.action)}>
          {strings.common.view}
        </Button>
      ),
    },
  ];

  return (
    <section className="grid gap-3" aria-labelledby="audit-log-heading">
      <div className="space-y-1">
        <h2 id="audit-log-heading" className="text-base font-semibold">
          {strings.settings.auditTitle}
        </h2>
        <p className="text-sm text-muted-foreground">{strings.settings.auditDescription}</p>
      </div>
      <DataTable
        caption={strings.settings.auditTitle}
        columns={columns}
        data={logs.data?.data}
        getRowId={(e) => e.id}
        isLoading={logs.isPending || resetting}
        error={resetting ? null : logs.error}
        onRetry={() => void logs.refetch()}
        onRowClick={setSelected}
        pagination={{ nextCursor: logs.data?.next_cursor, onPageChange: setCursor }}
        resetKey={resetKey}
        emptyState={
          <EmptyState
            icon={ScrollTextIcon}
            headingLevel={3}
            title={strings.settings.auditEmptyTitle}
            description={strings.settings.auditEmptyBody}
            action={
              <Button asChild variant="outline">
                <Link to="/apps">{strings.settings.auditEmptyAction}</Link>
              </Button>
            }
          />
        }
      />
      <AuditEntryDialog entry={selected} onClose={() => setSelected(null)} />
    </section>
  );
}
