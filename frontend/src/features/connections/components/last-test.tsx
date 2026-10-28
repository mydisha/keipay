import { DateTimeText } from "@/shared/ui/date-time-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import type { ConnectionListItem } from "../api/connections";

/** `Passed`, `Failed` or `Not tested` as text, with when the last test ran. */
export function LastTest({ connection }: { connection: Pick<ConnectionListItem, "last_test_status" | "last_tested_at"> }) {
  const status = connection.last_test_status ?? "not_tested";
  return (
    <span className="inline-flex flex-wrap items-center gap-2">
      <StatusBadge status={status} />
      {connection.last_test_status && connection.last_tested_at && (
        <DateTimeText value={connection.last_tested_at} className="text-muted-foreground" />
      )}
    </span>
  );
}
