import { RotateCwIcon, WebhookIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppName } from "@/features/apps/components/app-name";
import { useTenantNames } from "@/features/apps/api/tenants";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  DELIVERY_STATUSES,
  isDeliveryStatus,
  useDeliveries,
  useReplayDelivery,
  type DeliveryFilters,
  type WebhookDelivery,
} from "../api/deliveries";

const NO_FILTERS: DeliveryFilters = { status: null, tenantId: null };

export function OutboundDeliveries() {
  const [filters, setFilters] = useState<DeliveryFilters>(NO_FILTERS);
  const deliveries = useDeliveries(filters);
  const canReplay = usePermission("deliveries:replay");
  const replay = useReplayDelivery();
  const names = useTenantNames();
  const filtered = filters.status !== null || filters.tenantId !== null;
  const clearFilters = () => setFilters(NO_FILTERS);

  function onReplay(id: number) {
    replay.mutate(id, {
      onSuccess: () => toast.success(strings.webhooks.replayed(id)),
      onError: (error) => toast.error(errorMessage(error, strings.webhooks.replayFailed)),
    });
  }

  const allColumns: DataColumn<WebhookDelivery>[] = [
    {
      id: "id",
      header: strings.webhooks.delivery,
      card: "title",
      cell: (d) => <IdText value={d.id} prefix="#" label={strings.webhooks.delivery} />,
    },
    { id: "event", header: strings.webhooks.event, cell: (d) => <span className="font-mono text-xs">{d.event_type}</span> },
    {
      id: "payment",
      header: strings.webhooks.payment,
      cell: (d) => <IdText value={d.payment_id} prefix="#" label={strings.webhooks.payment} />,
    },
    { id: "app", header: strings.common.app, cell: (d) => <AppName id={d.tenant_id} names={names} /> },
    { id: "status", header: strings.webhooks.status, cell: (d) => <StatusBadge status={d.status} /> },
    {
      id: "attempts",
      header: strings.webhooks.attempts,
      align: "right",
      cell: (d) => <span className="tabular-nums">{d.attempts}</span>,
    },
    {
      id: "last_error",
      header: strings.webhooks.lastError,
      className: "max-w-56",
      cell: (d) =>
        d.last_error ? (
          <span className="block truncate text-danger-foreground" title={d.last_error}>
            {d.last_error}
          </span>
        ) : (
          <span className="text-muted-foreground">{strings.common.none}</span>
        ),
    },
    { id: "created", header: strings.webhooks.created, cell: (d) => <DateTimeText value={d.created_at} /> },
    {
      id: "replay",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (d) => {
        const busy = replay.isPending && replay.variables === d.id;
        return (
          <Button
            variant="outline"
            size="sm"
            onClick={() => onReplay(d.id)}
            disabled={busy}
            aria-label={strings.webhooks.replayLabel(d.id)}
          >
            <RotateCwIcon aria-hidden="true" className={busy ? "animate-spin" : undefined} />
            {busy ? strings.webhooks.replaying : strings.webhooks.replay}
          </Button>
        );
      },
    },
  ];
  const columns = canReplay ? allColumns : allColumns.filter((column) => column.id !== "replay");

  return (
    <DataTable
      caption={strings.webhooks.outbound}
      columns={columns}
      data={deliveries.data}
      getRowId={(d) => String(d.id)}
      isLoading={deliveries.isPending}
      error={deliveries.error}
      onRetry={() => void deliveries.refetch()}
      filters={[
        {
          id: "status",
          label: strings.webhooks.statusFilter,
          options: DELIVERY_STATUSES.map((s) => ({ value: s, label: strings.status[s] ?? s })),
          value: filters.status,
          onChange: (value) => setFilters((f) => ({ ...f, status: isDeliveryStatus(value) ? value : null })),
        },
      ]}
      toolbar={<AppFilterSelect value={filters.tenantId} onChange={(tenantId) => setFilters((f) => ({ ...f, tenantId }))} />}
      hasActiveFilters={filtered}
      onClearFilters={clearFilters}
      emptyState={
        filtered ? (
          <EmptyState
            icon={WebhookIcon}
            title={strings.webhooks.emptyFilteredTitle}
            description={strings.webhooks.emptyFilteredBody}
            action={
              <Button variant="outline" onClick={clearFilters}>
                {strings.common.clearFilters}
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={WebhookIcon}
            title={strings.webhooks.emptyTitle}
            description={strings.webhooks.emptyBody}
            action={
              <Button asChild variant="outline">
                <Link to="/payments">{strings.webhooks.emptyAction}</Link>
              </Button>
            }
          />
        )
      }
    />
  );
}
