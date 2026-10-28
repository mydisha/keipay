import { BanIcon, ChevronLeftIcon, CircleCheckIcon, PencilIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { IdText } from "@/shared/ui/id-text";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useDisableApp, useEnableApp, type App } from "../api/apps";
import { EditAppDialog } from "./edit-app-dialog";
import { EnvironmentBadge } from "./environment-badge";

export function BackToApps() {
  return (
    <Link
      to="/apps"
      className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11"
    >
      <ChevronLeftIcon className="size-4" aria-hidden="true" />
      {strings.apps.backToApps}
    </Link>
  );
}

/** The app's name, environment and status as text, slug and id, and the write actions. */
export function AppHeader({ app }: { app: App }) {
  const canWrite = usePermission("apps:write");
  const disable = useDisableApp();
  const enable = useEnableApp();
  const [editing, setEditing] = useState(false);
  const [confirming, setConfirming] = useState<"disable" | "enable" | null>(null);
  const isDisabled = app.status === "disabled";

  function confirm() {
    if (confirming === "disable") {
      disable.mutate(app.id, {
        onSuccess: (result) => {
          setConfirming(null);
          toast.success(strings.apps.disabled_toast(result.app.name));
        },
        onError: (error) => toast.error(errorMessage(error, strings.apps.disableFailed)),
      });
    } else if (confirming === "enable") {
      enable.mutate(app.id, {
        onSuccess: (result) => {
          setConfirming(null);
          toast.success(strings.apps.enabled_toast(result.app.name));
        },
        onError: (error) => toast.error(errorMessage(error, strings.apps.enableFailed)),
      });
    }
  }

  return (
    <div className="grid gap-3">
      <BackToApps />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h1 className="min-w-0 truncate text-xl font-semibold tracking-tight" title={app.name}>
              {app.name}
            </h1>
            <EnvironmentBadge environment={app.environment} />
            <StatusBadge status={app.status} />
          </div>
          <dl className="flex min-w-0 flex-wrap items-center gap-x-6 gap-y-1 text-sm">
            <div className="flex min-w-0 items-center gap-2">
              <dt className="text-muted-foreground">{strings.apps.slug}</dt>
              <dd className="truncate font-mono text-[0.8125rem]">{app.slug}</dd>
            </div>
            <div className="flex min-w-0 items-center gap-2">
              <dt className="text-muted-foreground">{strings.apps.appId}</dt>
              <dd className="min-w-0">
                <IdText value={app.id} label={strings.apps.appId} />
              </dd>
            </div>
          </dl>
        </div>
        {canWrite && (
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => setEditing(true)}>
              <PencilIcon aria-hidden="true" />
              {strings.apps.edit}
            </Button>
            {isDisabled ? (
              <Button variant="outline" onClick={() => setConfirming("enable")}>
                <CircleCheckIcon aria-hidden="true" />
                {strings.apps.enable}
              </Button>
            ) : (
              <Button variant="outline" onClick={() => setConfirming("disable")}>
                <BanIcon aria-hidden="true" />
                {strings.apps.disable}
              </Button>
            )}
          </div>
        )}
      </div>

      <EditAppDialog app={editing ? app : null} onClose={() => setEditing(false)} />
      <ConfirmDialog
        open={confirming === "disable"}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={strings.apps.disableTitle(app.name)}
        description={strings.apps.disableBody}
        confirmText={app.slug}
        confirmLabel={strings.apps.disable}
        pendingLabel={strings.apps.disabling}
        pending={disable.isPending}
        destructive
        onConfirm={confirm}
      />
      <ConfirmDialog
        open={confirming === "enable"}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={strings.apps.enableTitle(app.name)}
        description={strings.apps.enableBody}
        confirmLabel={strings.apps.enable}
        pendingLabel={strings.apps.enabling}
        pending={enable.isPending}
        onConfirm={confirm}
      />
    </div>
  );
}

export function AppHeaderSkeleton() {
  return (
    <div className="grid gap-3" aria-busy="true" data-testid="app-header-skeleton">
      <Skeleton className="h-4 w-24" />
      <Skeleton className="h-7 w-64 max-w-full" />
      <Skeleton className="h-4 w-80 max-w-full" />
    </div>
  );
}
