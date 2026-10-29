import { InboxIcon, PanelRightOpenIcon } from "lucide-react";
import { useId } from "react";
import { Link } from "react-router";
import { useConnectionFilterOptions, useProviders, providerLabel } from "@/features/connections/api/connections";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { DataTable, type DataColumn, type FilterChipGroup } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { Label } from "@/shared/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { INBOUND_RESULTS, isInboundResult, useInboundWebhooks, type InboundFilters, type InboundWebhook } from "../api/inbound";
import { resultLabel } from "../lib/inbound-labels";
import type { WebhooksSearch } from "../lib/webhooks-search";
import { environmentChips } from "./filter-chips";
import { ResultBadge, SignatureBadge } from "./inbound-badges";

const FILTER_KEYS = ["result", "connection_id"] as const;
const ALL = "all";

function inboundFilters(search: WebhooksSearch): InboundFilters {
  const result = search.get("result");
  return {
    result: isInboundResult(result) ? result : null,
    connectionId: search.get("connection_id"),
    environment: search.environment,
  };
}

function Mono({ value }: { value: string | null }) {
  if (!value) return <span className="text-muted-foreground">{strings.common.none}</span>;
  return (
    <span className="block max-w-full truncate font-mono text-[0.8125rem]" title={value}>
      {value}
    </span>
  );
}

function ConnectionFilterSelect({ value, onChange }: { value: string | null; onChange: (id: string | null) => void }) {
  const id = useId();
  const { data: connections = [] } = useConnectionFilterOptions();
  return (
    <div className="flex items-center gap-2">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {strings.webhooks.connectionFilter}
      </Label>
      <Select value={value ?? ALL} onValueChange={(next) => onChange(next === ALL ? null : next)}>
        <SelectTrigger id={id} className="h-8 w-56 max-w-full">
          <SelectValue />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{strings.webhooks.allConnections}</SelectItem>
          {connections.map((c) => (
            <SelectItem key={c.id} value={c.id}>
              {strings.common.appOption(c.name, strings.environments[c.environment] ?? c.environment)}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}

export function InboundWebhooks({ search }: { search: WebhooksSearch }) {
  const filters = inboundFilters(search);
  const inbound = useInboundWebhooks(filters);
  const list = loadMoreList(inbound);
  const providers = useProviders();

  const otherFilters = FILTER_KEYS.some((key) => search.get(key) !== null);
  const clearFilters = () => search.clearFilters(FILTER_KEYS);

  const columns: DataColumn<InboundWebhook>[] = [
    {
      id: "connection",
      header: strings.webhooks.connection,
      card: "title",
      className: "max-w-56",
      cell: (w) => (
        <span className="grid min-w-0">
          <Link
            to={`/connections/${w.connection.id}`}
            className="truncate rounded-sm underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
            title={w.connection.name}
            onClick={(event) => event.stopPropagation()}
          >
            {w.connection.name}
          </Link>
          <span className="truncate text-xs font-normal text-muted-foreground">{providerLabel(providers.data, w.provider)}</span>
        </span>
      ),
    },
    { id: "received", header: strings.webhooks.received, cell: (w) => <DateTimeText value={w.received_at} /> },
    { id: "event_key", header: strings.webhooks.eventKey, className: "max-w-48", cell: (w) => <Mono value={w.event_key} /> },
    { id: "provider_status", header: strings.webhooks.providerStatus, cell: (w) => <Mono value={w.provider_status} /> },
    { id: "reference", header: strings.webhooks.reference, className: "max-w-48", cell: (w) => <Mono value={w.reference} /> },
    { id: "signature", header: strings.webhooks.signature, cell: (w) => <SignatureBadge valid={w.signature_valid} /> },
    { id: "result", header: strings.webhooks.result, cell: (w) => <ResultBadge result={w.result} /> },
    {
      id: "view",
      header: strings.webhooks.details,
      card: "actions",
      align: "right",
      cell: (w) => (
        <Button variant="outline" size="sm" onClick={() => search.openDetail(w.id)} aria-label={strings.webhooks.viewInbound(w.id)}>
          <PanelRightOpenIcon aria-hidden="true" />
          {strings.webhooks.details}
        </Button>
      ),
    },
  ];

  const chips: FilterChipGroup[] = [
    environmentChips(filters.environment, search.setEnvironment),
    {
      id: "result",
      label: strings.webhooks.resultFilter,
      options: INBOUND_RESULTS.map((r) => ({ value: r, label: resultLabel(r) })),
      value: filters.result,
      onChange: (value) => search.setFilter("result", value),
    },
  ];

  return (
    <DataTable
      caption={strings.webhooks.inbound}
      columns={columns}
      data={list.rows}
      getRowId={(w) => w.id}
      isLoading={inbound.isPending}
      error={list.error}
      onRetry={() => void inbound.refetch()}
      loadMore={list.loadMore}
      onRowClick={(w) => search.openDetail(w.id)}
      filters={chips}
      toolbar={
        <ConnectionFilterSelect value={filters.connectionId} onChange={(id) => search.setFilter("connection_id", id)} />
      }
      hasActiveFilters={otherFilters || filters.environment !== null}
      onClearFilters={clearFilters}
      emptyState={
        otherFilters ? (
          <EmptyState
            icon={InboxIcon}
            title={strings.webhooks.inboundFilteredTitle}
            description={strings.webhooks.inboundFilteredBody}
            action={
              <Button variant="outline" onClick={clearFilters}>
                {strings.common.clearFilters}
              </Button>
            }
          />
        ) : (
          <EmptyState
            icon={InboxIcon}
            title={strings.webhooks.inboundEmptyTitle}
            description={
              filters.environment
                ? strings.webhooks.inboundEmptyEnvironmentBody(strings.environments[filters.environment]?.toLowerCase() ?? filters.environment)
                : strings.webhooks.inboundEmptyBody
            }
            action={
              <>
                {filters.environment && (
                  <Button variant="outline" onClick={() => search.setEnvironment(null)}>
                    {strings.webhooks.showAllEnvironments}
                  </Button>
                )}
                <Button asChild variant="outline">
                  <Link to="/connections">{strings.webhooks.viewConnections}</Link>
                </Button>
              </>
            }
          />
        )
      }
    />
  );
}
