import { BanIcon, CircleCheckIcon, PencilIcon } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { DetailHeader, DetailHeaderSkeleton } from "@/shared/ui/detail-header";
import { Fact, FactList } from "@/shared/ui/fact-list";
import { IdText } from "@/shared/ui/id-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useDisableApp, useEnableApp, type App } from "../api/apps";
import { EditAppDialog } from "./edit-app-dialog";
import { EnvironmentBadge } from "./environment-badge";

export const backToApps = { to: "/apps", label: strings.apps.backToApps };

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
    <>
      <DetailHeader
        back={backToApps}
        title={app.name}
        badges={
          <>
            <EnvironmentBadge environment={app.environment} />
            <StatusBadge status={app.status} />
          </>
        }
        facts={
          <FactList layout="inline">
            <Fact label={strings.apps.slug} mono>
              {app.slug}
            </Fact>
            <Fact label={strings.apps.appId}>
              <IdText value={app.id} label={strings.apps.appId} />
            </Fact>
          </FactList>
        }
        actions={
          canWrite && (
            <>
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
            </>
          )
        }
      />

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
    </>
  );
}

export function AppHeaderSkeleton() {
  return <DetailHeaderSkeleton testId="app-header-skeleton" />;
}
