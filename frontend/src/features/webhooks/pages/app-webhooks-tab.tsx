import { PlusIcon, TriangleAlertIcon, WebhookIcon } from "lucide-react";
import { useState } from "react";
import { useNavigate } from "react-router";
import { toast } from "sonner";
import { useDetailApp } from "@/features/apps/pages/app-detail-page";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { formatDateTimeWIB, parseDate } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { CopyButton } from "@/shared/ui/copy-button";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { SecretRevealDialog } from "@/shared/ui/secret-reveal-dialog";
import { Section } from "@/shared/ui/section";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  useDeleteWebhookEndpoint,
  useDisableWebhookEndpoint,
  useEnableWebhookEndpoint,
  useRotateWebhookSecret,
  useSendTestEvent,
  useWebhookEndpoints,
  type WebhookEndpoint,
  type WebhookEndpointSecret,
} from "../api/endpoints";
import { TagBadge, SubscriptionSummary } from "../components/endpoint-badges";
import { EndpointRowActions, type EndpointAction } from "../components/endpoint-row-actions";
import { EndpointSheet } from "../components/endpoint-sheet";
import { RotateSecretDialog } from "../components/rotate-secret-dialog";
import { SendTestDialog } from "../components/send-test-dialog";
import { urlHost } from "../lib/endpoint-format";
import { deliveryPath } from "../lib/webhooks-search";

interface Revealed {
  result: WebhookEndpointSecret;
  rotated: boolean;
}

function wib(value: string | null): string {
  const date = parseDate(value);
  return date ? formatDateTimeWIB(date) : strings.common.none;
}

function autoDisabledText(endpoint: WebhookEndpoint): string {
  return strings.endpoints.autoDisabled(endpoint.consecutive_failures, wib(endpoint.disabled_at));
}

/** The endpoint's current signing secret as a hint: `…abcd`, or "Migrated secret" before its first rotation. */
function SecretHint({ endpoint }: { endpoint: WebhookEndpoint }) {
  const expires = parseDate(endpoint.previous_secret_expires_at);
  return (
    <span className="grid justify-items-end gap-0.5 text-right md:justify-items-start md:text-left">
      <span className={endpoint.secret_last4 ? "font-mono text-[0.8125rem]" : "text-muted-foreground"}>
        {endpoint.secret_last4 ? strings.endpoints.secretHint(endpoint.secret_last4) : strings.endpoints.migratedSecret}
      </span>
      {expires && expires.getTime() > Date.now() && (
        <span className="text-xs text-muted-foreground">{strings.endpoints.previousSecretUntil(formatDateTimeWIB(expires))}</span>
      )}
    </span>
  );
}

/** `/apps/:id/webhooks`: the app's webhook endpoints. */
export function AppWebhooksTab() {
  const app = useDetailApp();
  const canWrite = usePermission("apps:write");
  const navigate = useNavigate();
  const endpoints = useWebhookEndpoints(app.id);
  const list = loadMoreList(endpoints);

  const rotate = useRotateWebhookSecret();
  const disable = useDisableWebhookEndpoint();
  const enable = useEnableWebhookEndpoint();
  const remove = useDeleteWebhookEndpoint();
  const sendTest = useSendTestEvent();

  const [sheet, setSheet] = useState<{ endpoint: WebhookEndpoint | null } | null>(null);
  const [action, setAction] = useState<{ type: EndpointAction; endpoint: WebhookEndpoint } | null>(null);
  const [revealed, setRevealed] = useState<Revealed | null>(null);

  const close = () => setAction(null);
  const target = (type: EndpointAction) => (action?.type === type ? action.endpoint : null);

  function onAction(type: EndpointAction, endpoint: WebhookEndpoint) {
    if (type === "edit") setSheet({ endpoint });
    else setAction({ type, endpoint });
  }

  function onEnable(endpoint: WebhookEndpoint) {
    enable.mutate(endpoint.id, {
      onSuccess: () => {
        close();
        toast.success(strings.endpoints.enabled_toast);
      },
      onError: (error) => toast.error(errorMessage(error, strings.endpoints.enableFailed)),
    });
  }

  const columns: DataColumn<WebhookEndpoint>[] = [
    {
      id: "endpoint",
      header: strings.endpoints.endpoint,
      card: "title",
      className: "max-w-80",
      cell: (e) => (
        <div className="grid min-w-0 gap-0.5">
          <span className="flex min-w-0 items-center gap-1">
            <span className="truncate font-mono text-[0.8125rem]" title={e.url}>
              {e.url}
            </span>
            <CopyButton value={e.url} label={strings.common.copyValue(strings.endpoints.url)} />
          </span>
          {e.description && (
            <span className="truncate text-xs font-normal text-muted-foreground" title={e.description}>
              {e.description}
            </span>
          )}
        </div>
      ),
    },
    {
      id: "events",
      header: strings.endpoints.subscriptions,
      className: "max-w-64",
      cell: (e) => <SubscriptionSummary events={e.events} />,
    },
    {
      id: "version",
      header: strings.endpoints.version,
      cell: (e) => (
        <span className="flex flex-wrap items-center justify-end gap-1 md:justify-start">
          <TagBadge mono>{e.api_version}</TagBadge>
          {e.legacy_signature && <TagBadge>{strings.endpoints.legacySignature}</TagBadge>}
          {e.migrated && <TagBadge title={strings.endpoints.migratedHint}>{strings.endpoints.migrated}</TagBadge>}
        </span>
      ),
    },
    {
      id: "status",
      header: strings.endpoints.status,
      cell: (e) => (
        <span className="grid justify-items-end gap-0.5 md:justify-items-start">
          <StatusBadge status={e.status} />
          {e.disabled_reason === "consecutive_failures" && (
            <span className="text-xs text-muted-foreground">{strings.endpoints.autoDisabledShort}</span>
          )}
        </span>
      ),
    },
    { id: "secret", header: strings.endpoints.secret, cell: (e) => <SecretHint endpoint={e} /> },
    { id: "created", header: strings.endpoints.created, cell: (e) => <DateTimeText value={e.created_at} /> },
  ];
  if (canWrite) {
    columns.push({
      id: "actions",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (e) => <EndpointRowActions endpoint={e} onAction={onAction} />,
    });
  }

  const addButton = canWrite ? (
    <Button onClick={() => setSheet({ endpoint: null })}>
      <PlusIcon aria-hidden="true" />
      {strings.endpoints.add}
    </Button>
  ) : undefined;

  const autoDisabled = (list.rows ?? []).filter((e) => e.disabled_reason === "consecutive_failures");
  const rotating = target("rotate");
  const deleting = target("delete");
  const toggling = target("disable") ?? target("enable");
  const isDisable = action?.type === "disable";

  return (
    <Section id="app-webhooks-heading" title={strings.endpoints.title} description={strings.endpoints.description} actions={addButton}>
      {autoDisabled.map((endpoint) => (
        <Alert key={endpoint.id} variant="warning">
          <TriangleAlertIcon aria-hidden="true" />
          <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
            <span className="min-w-0">
              <span className="block truncate font-mono text-[0.8125rem]" title={endpoint.url}>
                {endpoint.url}
              </span>
              <span>{autoDisabledText(endpoint)}</span>
            </span>
            {canWrite && (
              <Button variant="outline" size="sm" className="text-foreground" onClick={() => onAction("enable", endpoint)}>
                {strings.endpoints.enable}
              </Button>
            )}
          </AlertDescription>
        </Alert>
      ))}

      <DataTable
        caption={strings.endpoints.title}
        columns={columns}
        data={list.rows}
        getRowId={(e) => e.id}
        isLoading={endpoints.isPending}
        error={list.error}
        onRetry={() => void endpoints.refetch()}
        loadMore={list.loadMore}
        skeletonRows={3}
        emptyState={
          <EmptyState
            icon={WebhookIcon}
            headingLevel={3}
            title={strings.endpoints.emptyTitle}
            description={canWrite ? strings.endpoints.emptyBody : strings.endpoints.emptyBodyReadOnly}
            action={addButton}
          />
        }
      />

      <EndpointSheet
        app={app}
        target={sheet}
        onClose={() => setSheet(null)}
        onCreated={(result) => {
          setSheet(null);
          setRevealed({ result, rotated: false });
        }}
      />
      <SendTestDialog
        endpoint={target("test")}
        pending={sendTest.isPending}
        onClose={close}
        onSend={(endpoint, eventType) =>
          sendTest.mutate(
            { id: endpoint.id, eventType },
            {
              onSuccess: ({ delivery }) => {
                close();
                toast.success(strings.endpoints.testQueued, {
                  action: { label: strings.endpoints.viewDelivery, onClick: () => void navigate(deliveryPath(delivery.id)) },
                });
              },
              onError: (error) => toast.error(errorMessage(error, strings.endpoints.testFailed)),
            },
          )
        }
      />
      <RotateSecretDialog
        endpoint={rotating}
        pending={rotate.isPending}
        onClose={close}
        onConfirm={(endpoint, overlapHours) =>
          rotate.mutate(
            { id: endpoint.id, overlapHours },
            {
              onSuccess: (result) => {
                close();
                toast.success(strings.endpoints.rotated_toast);
                setRevealed({ result, rotated: true });
              },
              onError: (error) => toast.error(errorMessage(error, strings.endpoints.rotateFailed)),
            },
          )
        }
      />
      <ConfirmDialog
        open={toggling !== null}
        onOpenChange={(open) => !open && close()}
        title={isDisable ? strings.endpoints.disableTitle : strings.endpoints.enableTitle}
        description={isDisable ? strings.endpoints.disableBody : strings.endpoints.enableBody}
        confirmLabel={isDisable ? strings.endpoints.disable : strings.endpoints.enable}
        pendingLabel={isDisable ? strings.endpoints.disabling : strings.endpoints.enabling}
        pending={disable.isPending || enable.isPending}
        onConfirm={() => {
          if (!toggling) return;
          if (!isDisable) {
            onEnable(toggling);
            return;
          }
          disable.mutate(toggling.id, {
            onSuccess: () => {
              close();
              toast.success(strings.endpoints.disabled_toast);
            },
            onError: (error) => toast.error(errorMessage(error, strings.endpoints.disableFailed)),
          });
        }}
      />
      <ConfirmDialog
        open={deleting !== null}
        onOpenChange={(open) => !open && close()}
        title={strings.endpoints.deleteTitle}
        description={strings.endpoints.deleteBody}
        confirmText={deleting ? urlHost(deleting.url) : undefined}
        confirmLabel={strings.endpoints.delete}
        pendingLabel={strings.endpoints.deleting}
        pending={remove.isPending}
        destructive
        onConfirm={() =>
          deleting &&
          remove.mutate(deleting, {
            onSuccess: () => {
              close();
              toast.success(strings.endpoints.deleted_toast);
            },
            onError: (error) => toast.error(errorMessage(error, strings.endpoints.deleteFailed)),
          })
        }
      />
      <SecretRevealDialog
        open={revealed !== null}
        title={strings.endpoints.secretTitle}
        description={
          revealed?.rotated
            ? strings.endpoints.secretRotated(
                revealed.result.endpoint.previous_secret_expires_at ? wib(revealed.result.endpoint.previous_secret_expires_at) : null,
              )
            : strings.endpoints.secretCreated
        }
        secrets={revealed ? [{ label: strings.endpoints.secret, value: revealed.result.secret }] : []}
        onClose={() => setRevealed(null)}
      />
    </Section>
  );
}
