import { RotateCwIcon, SearchXIcon } from "lucide-react";
import { useId, useState } from "react";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { IdText } from "@/shared/ui/id-text";
import { JsonViewer } from "@/shared/ui/json-viewer";
import { SidePanel } from "@/shared/ui/side-panel";
import { StatusBadge } from "@/shared/ui/status-badge";
import { Timeline, type TimelineItem } from "@/shared/ui/timeline";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { useDelivery, useReplayDelivery, type WebhookDeliveryAttempt, type WebhookDeliveryDetail } from "../api/deliveries";
import { TagBadge } from "./endpoint-badges";
import { DetailSection, DetailSkeleton, Fact } from "./detail-parts";

export function isNotFound(error: unknown): boolean {
  return isApiError(error) && (error.code === "not_found" || error.code === "invalid_request");
}

/** The toast for a failed replay: `409 delivery_not_replayable` says why. */
export function replayErrorMessage(error: unknown): string {
  if (isApiError(error) && error.code === "delivery_not_replayable") {
    const reason = error.details.reason;
    if (typeof reason === "string" && strings.webhooks.replayReasons[reason]) return strings.webhooks.replayReasons[reason];
  }
  return errorMessage(error, strings.webhooks.replayFailed);
}

function attemptItem(attempt: WebhookDeliveryAttempt): TimelineItem {
  const ok = attempt.response_status !== null && attempt.response_status >= 200 && attempt.response_status < 300;
  return {
    id: String(attempt.attempt),
    time: attempt.started_at,
    title: strings.webhooks.attemptTitle(attempt.attempt),
    tone: ok ? "success" : "danger",
    statusLabel:
      attempt.response_status === null ? strings.webhooks.noResponse : strings.webhooks.httpStatus(attempt.response_status),
    body: <AttemptBody attempt={attempt} />,
  };
}

function AttemptBody({ attempt }: { attempt: WebhookDeliveryAttempt }) {
  const headers = Object.entries(attempt.request_headers);
  return (
    <div className="grid gap-2 text-foreground">
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
        <span className="tabular-nums">{strings.webhooks.duration(attempt.duration_ms)}</span>
        {attempt.manual && <TagBadge>{strings.webhooks.manual}</TagBadge>}
        <span className="min-w-0 truncate font-mono" title={attempt.url}>
          {attempt.url}
        </span>
      </div>
      {attempt.error && <p className="text-sm text-danger-foreground">{attempt.error}</p>}
      <details className="rounded-md border bg-card">
        <summary className="cursor-pointer px-2 py-1.5 text-sm font-medium max-md:min-h-11 max-md:py-3">
          {strings.webhooks.requestHeaders}
        </summary>
        <div className="overflow-x-auto border-t">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-left text-muted-foreground">
                <th scope="col" className="px-2 py-1 font-medium">
                  {strings.webhooks.headerName}
                </th>
                <th scope="col" className="px-2 py-1 font-medium">
                  {strings.webhooks.headerValue}
                </th>
              </tr>
            </thead>
            <tbody className="font-mono">
              {headers.map(([name, value]) => (
                <tr key={name} className="border-t align-top">
                  <td className="px-2 py-1 whitespace-nowrap">{name}</td>
                  <td className="px-2 py-1 break-all">{value}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </details>
      <details className="rounded-md border bg-card">
        <summary className="cursor-pointer px-2 py-1.5 text-sm font-medium max-md:min-h-11 max-md:py-3">
          {strings.webhooks.responseBody}
        </summary>
        <div className="border-t">
          {attempt.response_body ? (
            <pre className="max-h-60 overflow-auto p-2 font-mono text-xs whitespace-pre-wrap break-all">{attempt.response_body}</pre>
          ) : (
            <p className="p-2 text-xs text-muted-foreground">{strings.webhooks.emptyResponseBody}</p>
          )}
          {attempt.response_body_truncated && (
            <p className="border-t px-2 py-1.5 text-xs text-muted-foreground">{strings.webhooks.responseTruncated}</p>
          )}
        </div>
      </details>
    </div>
  );
}

function ReplayButton({ delivery }: { delivery: WebhookDeliveryDetail }) {
  const replay = useReplayDelivery();
  const [confirming, setConfirming] = useState(false);
  const hintId = useId();
  const pending = delivery.status === "PENDING";

  const button = (
    <Button
      variant="outline"
      size="sm"
      disabled={pending}
      aria-describedby={pending ? hintId : undefined}
      onClick={() => setConfirming(true)}
    >
      <RotateCwIcon aria-hidden="true" />
      {strings.webhooks.replay}
    </Button>
  );

  return (
    <>
      {pending ? (
        <Tooltip>
          <TooltipTrigger asChild>
            {/* A disabled button gets no pointer or focus events; the wrapper carries the tooltip. */}
            <span tabIndex={0} className="inline-flex rounded-md outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50">
              {button}
            </span>
          </TooltipTrigger>
          <TooltipContent>{strings.webhooks.replayPending}</TooltipContent>
        </Tooltip>
      ) : (
        button
      )}
      {pending && (
        <span id={hintId} className="sr-only">
          {strings.webhooks.replayPending}
        </span>
      )}
      <ConfirmDialog
        open={confirming}
        onOpenChange={setConfirming}
        title={strings.webhooks.replayTitle}
        description={strings.webhooks.replayBody}
        confirmLabel={strings.webhooks.replay}
        pendingLabel={strings.webhooks.replaying}
        pending={replay.isPending}
        onConfirm={() =>
          replay.mutate(delivery.id, {
            onSuccess: () => {
              setConfirming(false);
              toast.success(strings.webhooks.replayed);
            },
            onError: (error) => {
              setConfirming(false);
              toast.error(replayErrorMessage(error));
            },
          })
        }
      />
    </>
  );
}

function DeliveryDetail({ delivery }: { delivery: WebhookDeliveryDetail }) {
  const canReplay = usePermission("deliveries:replay");
  return (
    <div className="grid gap-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <StatusBadge status={delivery.status} />
          <span className="font-mono text-sm">{delivery.event_type}</span>
          {delivery.event?.test && <TagBadge>{strings.webhooks.testEvent}</TagBadge>}
        </div>
        {canReplay && <ReplayButton delivery={delivery} />}
      </div>

      <dl className="grid gap-3 text-sm">
        <Fact label={strings.webhooks.deliveryId}>
          <IdText value={delivery.id} label={strings.webhooks.deliveryId} />
        </Fact>
        <Fact label={strings.webhooks.eventId}>
          {delivery.event ? <IdText value={delivery.event.id} label={strings.webhooks.eventId} /> : strings.common.none}
        </Fact>
        <Fact label={strings.webhooks.paymentId}>
          {delivery.payment_id ? (
            <IdText value={delivery.payment_id} label={strings.webhooks.paymentId} />
          ) : (
            <span className="text-muted-foreground">{strings.webhooks.noPayment}</span>
          )}
        </Fact>
        <Fact label={strings.webhooks.endpoint}>
          {delivery.endpoint ? (
            <span className="grid min-w-0 gap-0.5">
              <span className="truncate font-mono text-[0.8125rem]" title={delivery.endpoint.url}>
                {delivery.endpoint.url}
              </span>
              <span className="flex flex-wrap items-center gap-2">
                <IdText value={delivery.endpoint.id} label={strings.webhooks.endpointId} />
                {delivery.endpoint.deleted && <TagBadge>{strings.webhooks.endpointDeleted}</TagBadge>}
              </span>
            </span>
          ) : (
            <span className="text-muted-foreground">{strings.webhooks.noEndpoint}</span>
          )}
        </Fact>
        <Fact label={strings.webhooks.attempts}>
          <span className="tabular-nums">{strings.webhooks.attemptsOf(delivery.attempts, delivery.max_attempts)}</span>
        </Fact>
        <Fact label={strings.webhooks.nextAttempt}>
          <DateTimeText value={delivery.status === "PENDING" ? delivery.next_attempt_at : null} />
        </Fact>
        <Fact label={strings.webhooks.created}>
          <DateTimeText value={delivery.created_at} />
        </Fact>
        {delivery.last_error && (
          <Fact label={strings.webhooks.lastError}>
            <span className="break-words text-danger-foreground">{delivery.last_error}</span>
          </Fact>
        )}
      </dl>

      <JsonViewer value={delivery.payload} label={strings.webhooks.payload} />

      <DetailSection title={strings.webhooks.attemptLog}>
        {delivery.attempt_log.length === 0 ? (
          <EmptyState
            headingLevel={3}
            className="py-6"
            title={strings.webhooks.noAttempts}
            description={strings.webhooks.noAttemptsBody}
          />
        ) : (
          <Timeline items={delivery.attempt_log.map(attemptItem)} />
        )}
      </DetailSection>
    </div>
  );
}

/** The delivery named by `?id=dlv_…`, in a side panel. */
export function DeliveryDetailSheet({ id, onClose }: { id: string | null; onClose: () => void }) {
  const delivery = useDelivery(id);
  let body;
  if (delivery.isPending) body = <DetailSkeleton />;
  else if (delivery.isError)
    body = isNotFound(delivery.error) ? (
      <EmptyState
        icon={SearchXIcon}
        headingLevel={3}
        title={strings.webhooks.deliveryNotFoundTitle}
        description={strings.webhooks.deliveryNotFoundBody}
      />
    ) : (
      <ErrorState error={delivery.error} onRetry={() => void delivery.refetch()} />
    );
  else body = <DeliveryDetail delivery={delivery.data} />;

  return (
    <SidePanel
      open={id !== null}
      onOpenChange={(open) => !open && onClose()}
      title={strings.webhooks.deliveryTitle}
      description={strings.webhooks.deliveryDescription}
    >
      {body}
    </SidePanel>
  );
}
