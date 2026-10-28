import { BoxesIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { useEnvironmentFilter } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn, type FilterChipGroup } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { PageHeader } from "@/shared/ui/page-header";
import { SecretRevealDialog } from "@/shared/ui/secret-reveal-dialog";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  APP_ENVIRONMENTS,
  APP_STATUSES,
  isAppEnvironment,
  isAppStatus,
  useApps,
  type App,
  type AppCreated,
  type AppStatus,
} from "../api/apps";
import { CreateAppDialog } from "../components/create-app-dialog";
import { EnvironmentBadge } from "../components/environment-badge";

// Chip groups here offer "All" as a chip because the environment filter does
// not start empty: it starts at the Test/Live mode.
const ALL = "all";

function createdSecrets(result: AppCreated) {
  return [
    { label: strings.apps.apiKey, value: result.api_key },
    { label: strings.apps.apiSecret, value: result.api_secret },
    { label: strings.apps.webhookSecret, value: result.webhook_secret },
  ];
}

export function AppsPage() {
  const canWrite = usePermission("apps:write");
  const navigate = useNavigate();
  const [environment, setEnvironment] = useEnvironmentFilter();
  const [status, setStatus] = useState<AppStatus | null>(null);
  const apps = useApps({ environment, status });
  const list = loadMoreList(apps);

  const [createOpen, setCreateOpen] = useState(false);
  const [created, setCreated] = useState<AppCreated | null>(null);

  const columns: DataColumn<App>[] = [
    {
      id: "name",
      header: strings.apps.name,
      card: "title",
      className: "max-w-72",
      cell: (app) => (
        <div className="grid min-w-0">
          <Link
            to={`/apps/${app.id}`}
            title={app.name}
            className="truncate rounded-sm font-medium underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
            // The row navigates too; one history entry is enough.
            onClick={(event) => event.stopPropagation()}
          >
            {app.name}
          </Link>
          <span className="truncate font-mono text-xs text-muted-foreground" title={app.slug}>
            {app.slug}
          </span>
        </div>
      ),
    },
    {
      id: "environment",
      header: strings.apps.environment,
      cell: (app) => <EnvironmentBadge environment={app.environment} />,
    },
    { id: "status", header: strings.apps.status, cell: (app) => <StatusBadge status={app.status} /> },
    {
      id: "id",
      header: strings.apps.appId,
      className: "max-w-56",
      cell: (app) => <IdText value={app.id} label={strings.apps.appId} />,
    },
    { id: "created", header: strings.apps.created, cell: (app) => <DateTimeText value={app.created_at} /> },
  ];

  const filters: FilterChipGroup[] = [
    {
      id: "environment",
      label: strings.apps.environmentFilter,
      options: [
        { value: ALL, label: strings.common.all },
        ...APP_ENVIRONMENTS.map((e) => ({ value: e, label: strings.environments[e] ?? e })),
      ],
      value: environment ?? ALL,
      onChange: (value) => setEnvironment(isAppEnvironment(value) ? value : null),
    },
    {
      id: "status",
      label: strings.apps.statusFilter,
      options: [
        { value: ALL, label: strings.common.all },
        ...APP_STATUSES.map((s) => ({ value: s, label: strings.status[s] ?? s })),
      ],
      value: status ?? ALL,
      onChange: (value) => setStatus(isAppStatus(value) ? value : null),
    },
  ];

  const createButton = canWrite ? (
    <Button onClick={() => setCreateOpen(true)}>
      <PlusIcon aria-hidden="true" />
      {strings.apps.create}
    </Button>
  ) : undefined;

  const clearFilters = () => {
    setEnvironment(null);
    setStatus(null);
  };

  let emptyState;
  if (environment !== null) {
    emptyState = (
      <EmptyState
        icon={BoxesIcon}
        title={strings.apps.emptyEnvironmentTitle(environment)}
        description={strings.apps.emptyEnvironmentBody}
        action={
          <>
            <Button variant="outline" onClick={() => setEnvironment(null)}>
              {strings.apps.showAllEnvironments}
            </Button>
            {createButton}
          </>
        }
      />
    );
  } else if (status !== null) {
    emptyState = (
      <EmptyState
        icon={BoxesIcon}
        title={strings.apps.emptyFilteredTitle}
        description={strings.apps.emptyFilteredBody}
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
        icon={BoxesIcon}
        title={strings.apps.emptyTitle}
        description={canWrite ? strings.apps.emptyBody : strings.apps.emptyBodyReadOnly}
        action={createButton}
      />
    );
  }

  return (
    <>
      <PageHeader title={strings.apps.title} description={strings.apps.subtitle} actions={createButton} />
      <DataTable
        caption={strings.apps.title}
        columns={columns}
        data={list.rows}
        getRowId={(app) => app.id}
        isLoading={apps.isPending}
        error={list.error}
        onRetry={() => void apps.refetch()}
        filters={filters}
        hasActiveFilters={environment !== null || status !== null}
        onClearFilters={clearFilters}
        loadMore={list.loadMore}
        onRowClick={(app) => void navigate(`/apps/${app.id}`)}
        emptyState={emptyState}
      />

      <CreateAppDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(result) => {
          setCreateOpen(false);
          toast.success(strings.apps.created_toast(result.app.name));
          setCreated(result);
        }}
      />
      <SecretRevealDialog
        open={created !== null}
        title={strings.apps.secretTitle}
        description={strings.apps.secretDescription}
        warning={strings.apps.secretWarning}
        secrets={created ? createdSecrets(created) : []}
        onClose={() => {
          const id = created?.app.id;
          setCreated(null);
          if (id) void navigate(`/apps/${id}`);
        }}
      />
    </>
  );
}
