import { PanelRightOpenIcon, WebhookIcon } from "lucide-react";
import { Link } from "react-router";
import { AppFilterSelect } from "@/features/apps/components/app-filter-select";
import { AppLabel } from "@/features/apps/components/app-label";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn, type FilterChipGroup } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  DELIVERY_STATUSES,
  isDeliveryStatus,
  useDeliveries,
  type DeliveryFilters,
  type WebhookDelivery,
} from "../api/deliveries";
import { useEventTypes, type EventType } from "../api/event-types";
import type { WebhooksSearch } from "../lib/webhooks-search";
import { TagBadge } from "./endpoint-badges";
import { environmentChips, RemovableChip } from "./filter-chips";

const FILTER_KEYS = ["status", "event_type", "app_id", "endpoint_id", "event_id"] as const;

function deliveryFilters(search: WebhooksSearch): DeliveryFilters {
  const status = search.get("status");
  return {
    status: isDeliveryStatus(status) ? status : null,
    // The server checks the value against the catalog (rule `oneof`).
    eventType: (search.get("event_type") as EventType | null) ?? null,
    appId: search.get("app_id"),
    endpointId: search.get("endpoint_id"),
    eventId: search.get("event_id"),
    environment: search.environment,
  };
}

export function EndpointCell({ delivery }: { delivery: WebhookDelivery }) {
  if (!delivery.endpoint) return <span className="text-muted-foreground">{strings.webhooks.noEndpoint}</span>;
  return (
    <span className="grid min-w-0 justify-items-end gap-0.5 md:justify-items-start">
      <span className="block max-w-full truncate font-mono text-[0.8125rem]" title={delivery.endpoint.url}>
        {delivery.endpoint.url}
      </span>
      {delivery.endpoint.deleted && <span className="text-xs text-muted-foreground">{strings.webhooks.endpointDeleted}</span>}
    </span>
  );
}

export function OutboundDeliveries({ search }: { search: WebhooksSearch }) {
  const filters = deliveryFilters(search);
  const deliveries = useDeliveries(filters);
  const list = loadMoreList(deliveries);
  const catalog = useEventTypes();

  const otherFilters = FILTER_KEYS.some((key) => search.get(key) !== null);
  const clearFilters = () => search.clearFilters(FILTER_KEYS);

  const columns: DataColumn<WebhookDelivery>[] = [
    {
      id: "event",
      header: strings.webhooks.eventType,
      card: "title",
      cell: (d) => (
        <span className="inline-flex flex-wrap items-center gap-2">
          <span className="font-mono text-[0.8125rem]">{d.event_type}</span>
          {d.event?.test && <TagBadge>{strings.webhooks.testEvent}</TagBadge>}
        </span>
      ),
    },
    { id: "app", header: strings.webhooks.app, className: "max-w-56", cell: (d) => <AppLabel app={d.app} /> },
    { id: "endpoint", header: strings.webhooks.endpoint, className: "max-w-64", cell: (d) => <EndpointCell delivery={d} /> },
    { id: "status", header: strings.webhooks.status, cell: (d) => <StatusBadge status={d.status} /> },
    {
      id: "attempts",
      header: strings.webhooks.attempts,
      align: "right",
      cell: (d) => <span className="tabular-nums">{strings.webhooks.attemptsOf(d.attempts, d.max_attempts)}</span>,
    },
    {
      id: "response",
      header: strings.webhooks.lastResponse,
      align: "right",
      cell: (d) =>
        d.last_response_status === null ? (
          <span className="text-muted-foreground">{strings.common.none}</span>
        ) : (
          <span className="font-mono tabular-nums">{d.last_response_status}</span>
        ),
    },
    {
      id: "next",
      header: strings.webhooks.nextAttempt,
      cell: (d) => <DateTimeText value={d.status === "PENDING" ? d.next_attempt_at : null} />,
    },
    { id: "created", header: strings.webhooks.created, cell: (d) => <DateTimeText value={d.created_at} /> },
    {
      id: "view",
      header: strings.webhooks.details,
      card: "actions",
      align: "right",
      cell: (d) => (
        <Button variant="outline" size="sm" onClick={() => search.openDetail(d.id)} aria-label={strings.webhooks.viewDelivery(d.id)}>
          <PanelRightOpenIcon aria-hidden="true" />
          {strings.webhooks.details}
        </Button>
      ),
    },
  ];

  const chips: FilterChipGroup[] = [
    environmentChips(filters.environment, search.setEnvironment),
    {
      id: "status",
      label: strings.webhooks.statusFilter,
      options: DELIVERY_STATUSES.map((s) => ({ value: s, label: strings.status[s] ?? s })),
      value: filters.status,
      onChange: (value) => search.setFilter("status", value),
    },
  ];
  if (catalog.data) {
    chips.push({
      id: "event_type",
      label: strings.webhooks.eventTypeFilter,
      options: catalog.data.data.map((t) => ({ value: t.type, label: t.type })),
      value: filters.eventType,
      onChange: (value) => search.setFilter("event_type", value),
    });
  }

  const endpointId = filters.endpointId;
  const eventId = filters.eventId;

  return (
    <DataTable
      caption={strings.webhooks.outbound}
      columns={columns}
      data={list.rows}
      getRowId={(d) => d.id}
      isLoading={deliveries.isPending}
      error={list.error}
      onRetry={() => void deliveries.refetch()}
      loadMore={list.loadMore}
      onRowClick={(d) => search.openDetail(d.id)}
      filters={chips}
      toolbar={
        <>
          <AppFilterSelect value={filters.appId} onChange={(appId) => search.setFilter("app_id", appId)} />
          {endpointId && (
            <RemovableChip label={strings.webhooks.endpointChip(endpointId)} onRemove={() => search.setFilter("endpoint_id", null)} />
          )}
          {eventId && <RemovableChip label={strings.webhooks.eventChip(eventId)} onRemove={() => search.setFilter("event_id", null)} />}
        </>
      }
      hasActiveFilters={otherFilters || filters.environment !== null}
      onClearFilters={clearFilters}
      emptyState={
        otherFilters ? (
          <EmptyState
            icon={WebhookIcon}
            title={strings.webhooks.outboundFilteredTitle}
            description={strings.webhooks.outboundFilteredBody}
            action={
              <Button variant="outline" onClick={clearFilters}>
                {strings.common.clearFilters}
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={WebhookIcon}
            title={strings.webhooks.outboundEmptyTitle}
            description={
              filters.environment
                ? strings.webhooks.outboundEmptyEnvironmentBody(strings.environments[filters.environment]?.toLowerCase() ?? filters.environment)
                : strings.webhooks.outboundEmptyBody
            }
            action={
              <>
                {filters.environment && (
                  <Button variant="outline" onClick={() => search.setEnvironment(null)}>
                    {strings.webhooks.showAllEnvironments}
                  </Button>
                )}
                <Button asChild variant="outline">
                  <Link to="/apps">{strings.webhooks.viewApps}</Link>
                </Button>
              </>
            }
          />
        )
      }
    />
  );
}
