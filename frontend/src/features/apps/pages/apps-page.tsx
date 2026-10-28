import { BoxesIcon, PlusIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { errorMessage } from "@/shared/api/error-message";
import type { components } from "@/shared/api/schema";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { DataTable, type DataColumn } from "@/shared/ui/data-table";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { EmptyState } from "@/shared/ui/empty-state";
import { IdText } from "@/shared/ui/id-text";
import { PageHeader } from "@/shared/ui/page-header";
import { SecretRevealDialog, type RevealedSecret } from "@/shared/ui/secret-reveal-dialog";
import { StatusBadge } from "@/shared/ui/status-badge";
import { tenantUpdateBody, useRotateTenant, useTenants, useUpdateTenant, type Tenant } from "../api/tenants";
import { AppRowActions } from "../components/app-row-actions";
import { CreateAppDialog } from "../components/create-app-dialog";
import { EditAppDialog } from "../components/edit-app-dialog";

type Credentials = components["schemas"]["TenantCredentials"];

function credentialSecrets(credentials: Credentials): RevealedSecret[] {
  return [
    { label: strings.apps.apiKey, value: credentials.api_key },
    { label: strings.apps.apiSecret, value: credentials.api_secret },
  ];
}

export function AppsPage() {
  const tenants = useTenants();
  const update = useUpdateTenant();
  const rotate = useRotateTenant();

  const [createOpen, setCreateOpen] = useState(false);
  const [editing, setEditing] = useState<Tenant | null>(null);
  const [rotating, setRotating] = useState<Tenant | null>(null);
  const [deactivating, setDeactivating] = useState<Tenant | null>(null);
  const [revealed, setRevealed] = useState<RevealedSecret[] | null>(null);

  function setActive(tenant: Tenant, active: boolean) {
    update.mutate(
      { id: tenant.id, body: tenantUpdateBody(tenant, { active }) },
      {
        onSuccess: () => {
          toast.success(active ? strings.apps.activated(tenant.name) : strings.apps.deactivated(tenant.name));
          setDeactivating(null);
        },
        onError: (error) => toast.error(errorMessage(error, strings.apps.updateFailed)),
      },
    );
  }

  function confirmRotate() {
    if (!rotating) return;
    rotate.mutate(rotating.id, {
      onSuccess: (result) => {
        setRotating(null);
        toast.success(strings.apps.rotated);
        setRevealed(credentialSecrets(result.credentials));
      },
      onError: (error) => toast.error(errorMessage(error, strings.apps.rotateFailed)),
    });
  }

  const columns: DataColumn<Tenant>[] = [
    {
      id: "name",
      header: strings.apps.name,
      card: "title",
      cell: (t) => (
        <div className="grid min-w-0">
          <span className="truncate font-medium">{t.name}</span>
          <span className="truncate font-mono text-xs text-muted-foreground">{t.slug}</span>
        </div>
      ),
    },
    {
      id: "api_key",
      header: strings.apps.apiKey,
      className: "max-w-56",
      cell: (t) => <IdText value={t.api_key} label={strings.apps.apiKey} />,
    },
    {
      id: "webhook_url",
      header: strings.apps.webhookUrl,
      className: "max-w-64",
      cell: (t) =>
        t.webhook_url ? (
          <span className="block truncate" title={t.webhook_url}>
            {t.webhook_url}
          </span>
        ) : (
          <span className="text-muted-foreground">{strings.common.none}</span>
        ),
    },
    {
      id: "status",
      header: strings.apps.status,
      cell: (t) => <StatusBadge status={t.active ? "active" : "inactive"} />,
    },
    { id: "created", header: strings.apps.created, cell: (t) => <DateTimeText value={t.created_at} /> },
    {
      id: "actions",
      header: strings.common.actions,
      card: "actions",
      align: "right",
      cell: (t) => (
        <AppRowActions
          tenant={t}
          onEdit={setEditing}
          onRotate={setRotating}
          onToggleActive={(tenant) => (tenant.active ? setDeactivating(tenant) : setActive(tenant, true))}
        />
      ),
    },
  ];

  const createButton = (
    <Button onClick={() => setCreateOpen(true)}>
      <PlusIcon aria-hidden="true" />
      {strings.apps.create}
    </Button>
  );

  return (
    <>
      <PageHeader title={strings.apps.title} description={strings.apps.subtitle} actions={createButton} />
      <DataTable
        caption={strings.apps.title}
        columns={columns}
        data={tenants.data}
        getRowId={(t) => String(t.id)}
        isLoading={tenants.isPending}
        error={tenants.error}
        onRetry={() => void tenants.refetch()}
        emptyState={
          <EmptyState
            icon={BoxesIcon}
            title={strings.apps.emptyTitle}
            description={strings.apps.emptyBody}
            action={createButton}
          />
        }
      />

      <CreateAppDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(tenant, credentials) => {
          setCreateOpen(false);
          toast.success(strings.apps.created_toast(tenant.name));
          setRevealed(credentialSecrets(credentials));
        }}
      />
      <EditAppDialog tenant={editing} onClose={() => setEditing(null)} />
      <ConfirmDialog
        open={rotating !== null}
        onOpenChange={(open) => !open && setRotating(null)}
        title={rotating ? strings.apps.rotateTitle(rotating.name) : ""}
        description={strings.apps.rotateBody}
        confirmText={rotating?.slug}
        confirmLabel={strings.apps.rotateConfirm}
        pendingLabel={strings.apps.rotating}
        pending={rotate.isPending}
        destructive
        onConfirm={confirmRotate}
      />
      <ConfirmDialog
        open={deactivating !== null}
        onOpenChange={(open) => !open && setDeactivating(null)}
        title={deactivating ? strings.apps.deactivateTitle(deactivating.name) : ""}
        description={strings.apps.deactivateBody}
        confirmLabel={strings.apps.deactivate}
        pending={update.isPending}
        destructive
        onConfirm={() => deactivating && setActive(deactivating, false)}
      />
      <SecretRevealDialog
        open={revealed !== null}
        title={strings.apps.secretTitle}
        description={strings.apps.secretDescription}
        secrets={revealed ?? []}
        onClose={() => setRevealed(null)}
      />
    </>
  );
}
