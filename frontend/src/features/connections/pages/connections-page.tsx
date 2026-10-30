import { PlusIcon } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { usePermission } from "@/features/auth/components/can";
import { EnvironmentBadge } from "@/features/apps/components/environment-badge";
import { useEnvironmentFilter } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn, type FilterChipGroup } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { PageHeader } from "@/shared/ui/page-header";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  CONNECTION_ENVIRONMENTS,
  CONNECTION_STATUSES,
  isConnectionEnvironment,
  isConnectionStatus,
  providerLabel,
  useConnections,
  useProviders,
  type ConnectionListItem,
  type ConnectionStatus,
} from "../api/connections";
import { LastTest } from "../components/last-test";
import { LegacyUrlBadge } from "../components/legacy-url-badge";

// The environment chip starts at the Test/Live mode, so every group offers
// "All" as a chip (see the dense table pattern in DESIGN.md).
const ALL = "all";

function AddConnectionButton({ environment }: { environment: string | null }) {
  const to = environment ? `/connections/new?environment=${environment}` : "/connections/new";
  return (
    <Button asChild>
      <Link to={to}>
        <PlusIcon aria-hidden="true" />
        {strings.connections.add}
      </Link>
    </Button>
  );
}

/** `/connections`: every gateway connection, filtered by environment, status and provider. */
export function ConnectionsPage() {
  const canWrite = usePermission("connections:write");
  const navigate = useNavigate();
  const [environment, setEnvironment] = useEnvironmentFilter();
  const [status, setStatus] = useState<ConnectionStatus | null>(null);
  const [provider, setProvider] = useState<string | null>(null);
  const providers = useProviders();
  const connections = useConnections({ environment, status, provider });
  const list = loadMoreList(connections);

  const columns: DataColumn<ConnectionListItem>[] = [
    {
      id: "name",
      header: strings.connections.name,
      card: "title",
      className: "max-w-72",
      cell: (connection) => (
        <div className="grid min-w-0 gap-0.5">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <Link
              to={`/connections/${connection.id}`}
              title={connection.name}
              className="truncate rounded-sm font-medium underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
              // The row navigates too; one history entry is enough.
              onClick={(event) => event.stopPropagation()}
            >
              {connection.name}
            </Link>
            {connection.legacy_webhook_alias && <LegacyUrlBadge />}
          </div>
          <IdText value={connection.id} label={strings.connections.connectionId} className="text-muted-foreground" />
        </div>
      ),
    },
    {
      id: "provider",
      header: strings.connections.provider,
      cell: (connection) => providerLabel(providers.data, connection.provider),
    },
    {
      id: "environment",
      header: strings.connections.environment,
      cell: (connection) => <EnvironmentBadge environment={connection.environment} />,
    },
    {
      id: "status",
      header: strings.connections.status,
      cell: (connection) => <StatusBadge status={connection.status} />,
    },
    { id: "last-test", header: strings.connections.lastTest, cell: (connection) => <LastTest connection={connection} /> },
    {
      id: "created",
      header: strings.connections.created,
      cell: (connection) => <DateTimeText value={connection.created_at} />,
    },
  ];

  const filters: FilterChipGroup[] = [
    {
      id: "environment",
      label: strings.connections.environmentFilter,
      options: [
        { value: ALL, label: strings.common.all },
        ...CONNECTION_ENVIRONMENTS.map((e) => ({ value: e, label: strings.environments[e] ?? e })),
      ],
      value: environment ?? ALL,
      onChange: (value) => setEnvironment(isConnectionEnvironment(value) ? value : null),
    },
    {
      id: "status",
      label: strings.connections.statusFilter,
      options: [
        { value: ALL, label: strings.common.all },
        ...CONNECTION_STATUSES.map((s) => ({ value: s, label: strings.status[s] ?? s })),
      ],
      value: status ?? ALL,
      onChange: (value) => setStatus(isConnectionStatus(value) ? value : null),
    },
  ];
  if (providers.data && providers.data.length > 0) {
    filters.push({
      id: "provider",
      label: strings.connections.providerFilter,
      options: [
        { value: ALL, label: strings.common.all },
        ...(providers.data ?? []).map((p) => ({ value: p.name, label: p.display_name })),
      ],
      value: provider ?? ALL,
      onChange: (value) => setProvider(value === null || value === ALL ? null : value),
    });
  }

  const addButton = canWrite ? <AddConnectionButton environment={environment} /> : undefined;

  const clearFilters = () => {
    setEnvironment(null);
    setStatus(null);
    setProvider(null);
  };

  let emptyState;
  if (environment !== null && status === null && provider === null) {
    emptyState = (
      <EmptyState
        title={strings.connections.emptyEnvironmentTitle(environment)}
        description={strings.connections.emptyEnvironmentBody}
        action={
          <>
            <Button variant="outline" onClick={() => setEnvironment(null)}>
              {strings.connections.showAllEnvironments}
            </Button>
            {addButton}
          </>
        }
      />
    );
  } else if (environment !== null || status !== null || provider !== null) {
    emptyState = (
      <EmptyState
        title={strings.connections.emptyFilteredTitle}
        description={strings.connections.emptyFilteredBody}
        action={
          <Button variant="outline" onClick={clearFilters}>
            {strings.common.clearFilters}
          </Button>
        }
      />
    );
  } else {
    emptyState = (
      <EmptyState
        title={strings.connections.emptyTitle}
        description={canWrite ? strings.connections.emptyBody : strings.connections.emptyBodyReadOnly}
        action={addButton}
      />
    );
  }

  return (
    <>
      <PageHeader title={strings.connections.title} description={strings.connections.subtitle} actions={addButton} />
      <DataTable
        caption={strings.connections.title}
        columns={columns}
        data={list.rows}
        getRowId={(connection) => connection.id}
        isLoading={connections.isPending}
        error={list.error}
        onRetry={() => void connections.refetch()}
        filters={filters}
        hasActiveFilters={environment !== null || status !== null || provider !== null}
        onClearFilters={clearFilters}
        loadMore={list.loadMore}
        onRowClick={(connection) => void navigate(`/connections/${connection.id}`)}
        emptyState={emptyState}
      />
    </>
  );
}
