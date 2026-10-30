import { PlusIcon } from "lucide-react";
import { Link } from "react-router";
import { EnvironmentBadge } from "@/features/apps/components/environment-badge";
import { Can, usePermission } from "@/features/auth/components/can";
import { providerLabel, useProviders, type ConnectionListItem } from "@/features/connections/api/connections";
import { LastTest } from "@/features/connections/components/last-test";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card, CardAction, CardContent, CardFooter, CardHeader, CardTitle } from "@/shared/ui/card";
import { EmptyState } from "@/shared/ui/empty-state";
import { StatusBadge } from "@/shared/ui/status-badge";

const MAX_ROWS = 5;

/** Up to five connections of the environment, one ruled line each, with their status and last test. */
export function ConnectionsHealthCard({ connections }: { connections: ConnectionListItem[] }) {
  const canWrite = usePermission("connections:write");
  const providers = useProviders();
  const shown = connections.slice(0, MAX_ROWS);
  const more = connections.length - shown.length;

  return (
    <Card className="overflow-hidden">
      <CardHeader className="items-center">
        <CardTitle>{strings.dashboard.healthTitle}</CardTitle>
        {shown.length > 0 && (
          <CardAction className="row-span-1 self-center">
            <Link
              to="/connections"
              className="rounded-sm text-sm text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              {strings.dashboard.viewConnections}
            </Link>
          </CardAction>
        )}
      </CardHeader>
      <CardContent className={shown.length > 0 ? "p-0" : undefined}>
        {shown.length === 0 ? (
          <EmptyState
            variant="inline"
            headingLevel={3}
            title={strings.dashboard.healthEmptyTitle}
            description={canWrite ? strings.dashboard.healthEmptyBody : strings.dashboard.healthEmptyBodyReadOnly}
            action={
              <Can permission="connections:write">
                <Button asChild variant="outline">
                  <Link to="/connections/new">
                    <PlusIcon aria-hidden="true" />
                    {strings.dashboard.addConnection}
                  </Link>
                </Button>
              </Can>
            }
          />
        ) : (
          <ul aria-label={strings.dashboard.healthTitle} className="divide-y">
            {shown.map((connection) => (
              <li key={connection.id} className="grid gap-1 px-(--card-pad) py-3">
                <span className="flex min-w-0 flex-wrap items-center gap-2">
                  <Link
                    to={`/connections/${connection.id}`}
                    title={connection.name}
                    className="truncate rounded-sm font-mono text-[0.8125rem] font-medium underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
                  >
                    {connection.name}
                  </Link>
                  <EnvironmentBadge environment={connection.environment} />
                  {connection.status !== "active" && <StatusBadge status={connection.status} />}
                </span>
                <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                  <span>{providerLabel(providers.data, connection.provider)}</span>
                  <LastTest connection={connection} />
                </span>
              </li>
            ))}
          </ul>
        )}
      </CardContent>
      {more > 0 && <CardFooter className="text-sm text-muted-foreground">{strings.dashboard.moreConnections(more)}</CardFooter>}
    </Card>
  );
}
