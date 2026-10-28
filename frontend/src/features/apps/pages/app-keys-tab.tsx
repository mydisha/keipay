import { KeyRoundIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { loadMoreList } from "@/shared/lib/use-cursor-pages";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { SecretRevealDialog } from "@/shared/ui/secret-reveal-dialog";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useApiKeys, useRevokeApiKey, useRotateApiKey, type ApiKey, type ApiKeyCreated } from "../api/apps";
import { CreateKeyDialog } from "../components/create-key-dialog";
import { keyHint, KeyRowActions, type KeyAction } from "../components/key-row-actions";
import { useDetailApp } from "./app-detail-page";

interface Revealed {
  result: ApiKeyCreated;
  rotated: boolean;
}

function OptionalTime({ value }: { value: string | null }) {
  return value ? <DateTimeText value={value} /> : <span className="text-muted-foreground">{strings.common.never}</span>;
}

/** `/apps/:id/keys`: every key of the app, including revoked and expired ones. */
export function AppKeysTab() {
  const app = useDetailApp();
  const canWrite = usePermission("apps:write");
  const keys = useApiKeys(app.id);
  const list = loadMoreList(keys);
  const rotate = useRotateApiKey();
  const revoke = useRevokeApiKey();

  const [creating, setCreating] = useState(false);
  const [confirming, setConfirming] = useState<{ action: KeyAction; key: ApiKey } | null>(null);
  const [revealed, setRevealed] = useState<Revealed | null>(null);

  function confirm() {
    if (!confirming) return;
    const { action, key } = confirming;
    const variables = { appId: app.id, keyId: key.id };
    if (action === "rotate") {
      rotate.mutate(variables, {
        onSuccess: (result) => {
          setConfirming(null);
          toast.success(strings.keys.rotated_toast(key.label));
          setRevealed({ result, rotated: true });
        },
        // validation_failed (revoked, expired or the 10-key limit) carries a readable server message.
        onError: (error) => {
          setConfirming(null);
          toast.error(errorMessage(error, strings.keys.rotateFailed));
        },
      });
    } else {
      revoke.mutate(variables, {
        onSuccess: () => {
          setConfirming(null);
          toast.success(strings.keys.revoked_toast(key.label));
        },
        onError: (error) => toast.error(errorMessage(error, strings.keys.revokeFailed)),
      });
    }
  }

  const allColumns: DataColumn<ApiKey>[] = [
    {
      id: "label",
      header: strings.keys.label,
      card: "title",
      className: "max-w-56",
      cell: (k) => (
        <span className="block truncate font-medium" title={k.label}>
          {k.label}
        </span>
      ),
    },
    {
      id: "key",
      header: strings.keys.key,
      cell: (k) => <span className="font-mono text-[0.8125rem] whitespace-nowrap">{keyHint(k)}</span>,
    },
    { id: "status", header: strings.keys.status, cell: (k) => <StatusBadge status={k.status} /> },
    { id: "last_used", header: strings.keys.lastUsed, cell: (k) => <OptionalTime value={k.last_used_at} /> },
    { id: "expires", header: strings.keys.expires, cell: (k) => <OptionalTime value={k.expires_at} /> },
    { id: "created", header: strings.keys.created, cell: (k) => <DateTimeText value={k.created_at} /> },
    {
      id: "actions",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (k) => <KeyRowActions apiKey={k} onAction={(action, key) => setConfirming({ action, key })} />,
    },
  ];
  const columns = canWrite ? allColumns : allColumns.filter((column) => column.id !== "actions");

  const createButton = canWrite ? (
    <Button onClick={() => setCreating(true)}>
      <PlusIcon aria-hidden="true" />
      {strings.keys.create}
    </Button>
  ) : undefined;

  const target = confirming?.key;
  const isRevoke = confirming?.action === "revoke";

  return (
    <section className="grid gap-3" aria-labelledby="app-keys-heading">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <h2 id="app-keys-heading" className="text-base font-semibold">
            {strings.keys.title}
          </h2>
          <p className="text-sm text-muted-foreground">{strings.keys.description}</p>
        </div>
        {createButton}
      </div>
      <DataTable
        caption={strings.keys.title}
        columns={columns}
        data={list.rows}
        getRowId={(k) => k.id}
        isLoading={keys.isPending}
        error={list.error}
        onRetry={() => void keys.refetch()}
        loadMore={list.loadMore}
        emptyState={
          <EmptyState
            icon={KeyRoundIcon}
            headingLevel={3}
            title={strings.keys.emptyTitle}
            description={canWrite ? strings.keys.emptyBody : strings.keys.emptyBodyReadOnly}
            action={createButton}
          />
        }
      />

      <CreateKeyDialog
        appId={app.id}
        open={creating}
        onOpenChange={setCreating}
        onCreated={(result) => {
          setCreating(false);
          toast.success(strings.keys.created_toast(result.key.label));
          setRevealed({ result, rotated: false });
        }}
      />
      <ConfirmDialog
        open={confirming !== null}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={target ? (isRevoke ? strings.keys.revokeTitle(target.label) : strings.keys.rotateTitle(target.label)) : ""}
        description={isRevoke ? strings.keys.revokeBody : strings.keys.rotateBody}
        confirmText={isRevoke ? target?.label : undefined}
        confirmLabel={isRevoke ? strings.keys.revoke : strings.keys.rotateConfirm}
        pendingLabel={isRevoke ? strings.keys.revoking : strings.keys.rotating}
        pending={rotate.isPending || revoke.isPending}
        destructive={isRevoke}
        onConfirm={confirm}
      />
      <SecretRevealDialog
        open={revealed !== null}
        title={strings.keys.secretTitle}
        description={revealed?.rotated ? strings.keys.rotatedDescription : strings.keys.secretDescription}
        secrets={
          revealed
            ? [
                { label: strings.apps.apiKey, value: revealed.result.api_key },
                { label: strings.apps.apiSecret, value: revealed.result.api_secret },
              ]
            : []
        }
        onClose={() => setRevealed(null)}
      />
    </section>
  );
}
