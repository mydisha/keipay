import { SearchXIcon } from "lucide-react";
import { Link } from "react-router";
import { providerLabel, useProviders } from "@/features/connections/api/connections";
import { strings } from "@/shared/lib/strings";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { IdText } from "@/shared/ui/id-text";
import { JsonViewer } from "@/shared/ui/json-viewer";
import { SidePanel } from "@/shared/ui/side-panel";
import { useInboundWebhook, type InboundWebhookDetail } from "../api/inbound";
import { parseJsonBody } from "../lib/inbound-labels";
import { eventDeliveriesPath } from "../lib/webhooks-search";
import { isNotFound } from "./delivery-detail-sheet";
import { DetailSection, DetailSkeleton, Fact, Facts } from "./detail-parts";
import { ResultBadge, SignatureBadge } from "./inbound-badges";

const linkClass =
  "rounded-sm text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50";

function MonoOrNone({ value, label }: { value: string | null; label: string }) {
  return value ? <IdText value={value} label={label} /> : <span className="text-muted-foreground">{strings.common.none}</span>;
}

function RawBody({ inbound }: { inbound: InboundWebhookDetail }) {
  const parsed = parseJsonBody(inbound.raw_body);
  return (
    <DetailSection title={strings.webhooks.rawBody}>
      {parsed ? (
        <JsonViewer value={parsed.value} label={strings.webhooks.rawBody} defaultOpen />
      ) : (
        <pre
          data-testid="raw-body-text"
          className="max-h-80 overflow-auto rounded-md border bg-card p-3 font-mono text-xs whitespace-pre-wrap break-all"
        >
          {inbound.raw_body}
        </pre>
      )}
      {inbound.raw_body_truncated && <p className="text-xs text-muted-foreground">{strings.webhooks.rawBodyTruncated}</p>}
    </DetailSection>
  );
}

function InboundDetail({ inbound }: { inbound: InboundWebhookDetail }) {
  const providers = useProviders();
  return (
    <div className="grid gap-6">
      <div className="grid gap-2">
        <div className="flex flex-wrap items-center gap-2">
          <ResultBadge result={inbound.result} />
          <SignatureBadge valid={inbound.signature_valid} />
        </div>
        {inbound.result_detail && <p className="text-sm">{inbound.result_detail}</p>}
      </div>
      <Facts>
        <Fact label={strings.webhooks.inboundId}>
          <IdText value={inbound.id} label={strings.webhooks.inboundId} />
        </Fact>
        <Fact label={strings.webhooks.connection}>
          <span className="flex flex-wrap items-center gap-2">
            <Link to={`/connections/${inbound.connection.id}`} className={linkClass}>
              {inbound.connection.name}
            </Link>
            <span className="text-muted-foreground">{providerLabel(providers.data, inbound.provider)}</span>
          </span>
        </Fact>
        <Fact label={strings.webhooks.received}>
          <DateTimeText value={inbound.received_at} />
        </Fact>
        <Fact label={strings.webhooks.eventKey}>
          <MonoOrNone value={inbound.event_key} label={strings.webhooks.eventKey} />
        </Fact>
        <Fact label={strings.webhooks.providerStatus}>
          {inbound.provider_status ? (
            <span className="font-mono text-[0.8125rem]">{inbound.provider_status}</span>
          ) : (
            <span className="text-muted-foreground">{strings.common.none}</span>
          )}
        </Fact>
        <Fact label={strings.webhooks.reference}>
          <MonoOrNone value={inbound.reference} label={strings.webhooks.reference} />
        </Fact>
        <Fact label={strings.webhooks.paymentId}>
          <MonoOrNone value={inbound.payment_id} label={strings.webhooks.paymentId} />
        </Fact>
        <Fact label={strings.webhooks.eventId}>
          {inbound.event ? (
            <span className="flex flex-wrap items-center gap-2">
              <IdText value={inbound.event.id} label={strings.webhooks.eventId} />
              <Link to={eventDeliveriesPath(inbound.event.id)} className={linkClass}>
                {strings.webhooks.viewEventDeliveries}
              </Link>
            </span>
          ) : (
            <span className="text-muted-foreground">{strings.common.none}</span>
          )}
        </Fact>
      </Facts>
      <RawBody inbound={inbound} />
    </div>
  );
}

/** The inbound webhook named by `?id=inb_…`, in a side panel. */
export function InboundDetailSheet({ id, onClose }: { id: string | null; onClose: () => void }) {
  const inbound = useInboundWebhook(id);
  let body;
  if (inbound.isPending) body = <DetailSkeleton />;
  else if (inbound.isError)
    body = isNotFound(inbound.error) ? (
      <EmptyState
        icon={SearchXIcon}
        headingLevel={3}
        title={strings.webhooks.inboundNotFoundTitle}
        description={strings.webhooks.inboundNotFoundBody}
      />
    ) : (
      <ErrorState error={inbound.error} onRetry={() => void inbound.refetch()} />
    );
  else body = <InboundDetail inbound={inbound.data} />;

  return (
    <SidePanel
      open={id !== null}
      onOpenChange={(open) => !open && onClose()}
      title={strings.webhooks.inboundTitle}
      description={strings.webhooks.inboundDescription}
    >
      {body}
    </SidePanel>
  );
}
