import { EnvironmentBadge } from "@/features/apps/components/environment-badge";
import type { ReconciliationRun } from "../api/runs";

/** A run's connection name and its environment. */
export function RunConnection({ run }: { run: Pick<ReconciliationRun, "connection"> }) {
  return (
    <span className="inline-flex max-w-full min-w-0 items-center gap-2">
      <span className="truncate font-mono text-[0.8125rem]" title={run.connection.name}>
        {run.connection.name}
      </span>
      <EnvironmentBadge environment={run.connection.environment} />
    </span>
  );
}
