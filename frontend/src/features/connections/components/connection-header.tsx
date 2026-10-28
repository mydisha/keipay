import { BanIcon, CircleCheckIcon, Loader2Icon, RotateCwIcon } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { EnvironmentBadge } from "@/features/apps/components/environment-badge";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { ConfirmDialog } from "@/shared/ui/confirm-dialog";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Skeleton } from "@/shared/ui/skeleton";
import { StatusBadge } from "@/shared/ui/status-badge";
import {
  useDisableConnection,
  useEnableConnection,
  useTestConnection,
  type AppSummary,
  type Connection,
} from "../api/connections";
import { BackToConnections } from "./back-to-connections";
import { LegacyUrlBadge } from "./legacy-url-badge";

interface InUse {
  apps: AppSummary[];
  count: number;
}

function isAppSummary(value: unknown): value is AppSummary {
  return typeof value === "object" && value !== null && "id" in value && "name" in value;
}

/** The apps a `409 connection_in_use` names, or null for any other error. */
function inUseFrom(error: unknown): InUse | null {
  if (!isApiError(error) || error.code !== "connection_in_use") return null;
  const apps = Array.isArray(error.details.apps) ? error.details.apps.filter(isAppSummary) : [];
  const count = typeof error.details.app_count === "number" ? error.details.app_count : apps.length;
  return { apps, count };
}

/** Name, provider, environment and status as text, and the write actions. */
export function ConnectionHeader({ connection, providerName }: { connection: Connection; providerName: string }) {
  const canWrite = usePermission("connections:write");
  const test = useTestConnection();
  const disable = useDisableConnection();
  const enable = useEnableConnection();
  const [confirming, setConfirming] = useState<"disable" | "enable" | null>(null);
  const [inUse, setInUse] = useState<InUse | null>(null);
  const isDisabled = connection.status === "disabled";

  function runTest() {
    test.mutate(connection.id, {
      onSuccess: (result) => {
        if (result.result === "ok") toast.success(strings.connections.testPassed(result.message));
        else toast.error(strings.connections.testFailed(result.message));
      },
      onError: (error) => toast.error(errorMessage(error, strings.connections.testRequestFailed)),
    });
  }

  function confirm() {
    if (confirming === "disable") {
      disable.mutate(connection.id, {
        onSuccess: (result) => {
          setConfirming(null);
          toast.success(strings.connections.disabled_toast(result.connection.name));
        },
        onError: (error) => {
          const apps = inUseFrom(error);
          if (apps) {
            setConfirming(null);
            setInUse(apps);
          } else {
            toast.error(errorMessage(error, strings.connections.disableFailed));
          }
        },
      });
    } else if (confirming === "enable") {
      enable.mutate(connection.id, {
        onSuccess: (result) => {
          setConfirming(null);
          toast.success(strings.connections.enabled_toast(result.connection.name));
        },
        onError: (error) => toast.error(errorMessage(error, strings.connections.enableFailed)),
      });
    }
  }

  return (
    <div className="grid gap-3">
      <BackToConnections />
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="grid min-w-0 gap-2">
          <div className="flex min-w-0 flex-wrap items-center gap-2">
            <h1 className="min-w-0 truncate font-mono text-xl font-semibold tracking-tight" title={connection.name}>
              {connection.name}
            </h1>
            <EnvironmentBadge environment={connection.environment} />
            <StatusBadge status={connection.status} />
            {connection.legacy_webhook_alias && <LegacyUrlBadge />}
          </div>
          <p className="text-sm text-muted-foreground">{providerName}</p>
        </div>
        {canWrite && (
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={runTest} disabled={test.isPending}>
              {test.isPending ? (
                <Loader2Icon className="animate-spin" aria-hidden="true" />
              ) : (
                <RotateCwIcon aria-hidden="true" />
              )}
              {test.isPending ? strings.connections.testing : strings.connections.testConnection}
            </Button>
            {isDisabled ? (
              <Button variant="outline" onClick={() => setConfirming("enable")}>
                <CircleCheckIcon aria-hidden="true" />
                {strings.connections.enable}
              </Button>
            ) : (
              <Button variant="outline" onClick={() => setConfirming("disable")}>
                <BanIcon aria-hidden="true" />
                {strings.connections.disable}
              </Button>
            )}
          </div>
        )}
      </div>

      <ConfirmDialog
        open={confirming === "disable"}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={strings.connections.disableTitle(connection.name)}
        description={strings.connections.disableBody}
        confirmText={connection.name}
        confirmLabel={strings.connections.disable}
        pendingLabel={strings.connections.disabling}
        pending={disable.isPending}
        destructive
        onConfirm={confirm}
      />
      <ConfirmDialog
        open={confirming === "enable"}
        onOpenChange={(open) => !open && setConfirming(null)}
        title={strings.connections.enableTitle(connection.name)}
        description={strings.connections.enableBody}
        confirmLabel={strings.connections.enable}
        pendingLabel={strings.connections.enabling}
        pending={enable.isPending}
        onConfirm={confirm}
      />
      <InUseDialog inUse={inUse} onClose={() => setInUse(null)} />
    </div>
  );
}

function InUseDialog({ inUse, onClose }: { inUse: InUse | null; onClose: () => void }) {
  const more = inUse ? inUse.count - inUse.apps.length : 0;
  return (
    <Dialog open={inUse !== null} onOpenChange={(open) => !open && onClose()}>
      {inUse && (
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{strings.connections.inUseTitle}</DialogTitle>
            <DialogDescription>{strings.connections.inUseBody(inUse.count)}</DialogDescription>
          </DialogHeader>
          <ul className="grid gap-1 text-sm">
            {inUse.apps.map((app) => (
              <li key={app.id} className="flex min-w-0 items-center gap-2 max-md:min-h-11">
                <Link
                  to={`/apps/${app.id}`}
                  className="truncate rounded-sm font-medium text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
                >
                  {app.name}
                </Link>
                <EnvironmentBadge environment={app.environment} />
              </li>
            ))}
          </ul>
          {more > 0 && <p className="text-sm text-muted-foreground">{strings.connections.inUseMore(more)}</p>}
          <DialogFooter>
            <Button variant="outline" onClick={onClose}>
              {strings.common.close}
            </Button>
          </DialogFooter>
        </DialogContent>
      )}
    </Dialog>
  );
}

export function ConnectionHeaderSkeleton() {
  return (
    <div className="grid gap-3" aria-busy="true" data-testid="connection-header-skeleton">
      <Skeleton className="h-4 w-32" />
      <Skeleton className="h-7 w-64 max-w-full" />
      <Skeleton className="h-4 w-40 max-w-full" />
    </div>
  );
}
