import { Loader2Icon, RotateCwIcon, TriangleAlertIcon } from "lucide-react";
import { useId, useState } from "react";
import { Link } from "react-router";
import { toast } from "sonner";
import { usePermissions } from "@/features/auth/components/can";
import { useConnectionOptions, type ConnectionSummary } from "@/features/connections/api/connections";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import { useUpdateApp, type App } from "../api/apps";

// Radix Select can't use "" as an item value.
const NONE = "none";

const FIELD_RULE_MESSAGES: Record<string, string> = {
  public_id: strings.apps.defaultConnectionInvalid,
  exists: strings.apps.defaultConnectionExists,
  environment: strings.apps.defaultConnectionEnvironment,
  status: strings.apps.defaultConnectionStatus,
};

/** The message for a `default_connection_id` field error, or null for any other failure. */
function fieldMessage(error: unknown): string | null {
  if (!isApiError(error) || error.code !== "invalid_request") return null;
  const field = error.fields.find((f) => f.field === "default_connection_id");
  if (!field) return null;
  return FIELD_RULE_MESSAGES[field.rule] ?? strings.common.invalidField(field.rule);
}

function ConnectionLink({ connection }: { connection: ConnectionSummary }) {
  return (
    <Link
      to={`/connections/${connection.id}`}
      className="rounded-sm font-mono text-[0.8125rem] text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
    >
      {connection.name}
    </Link>
  );
}

/** The connection `POST /v1/payments` uses for this app: a select for writers, a link for everyone else. */
export function DefaultConnection({ app }: { app: App }) {
  const can = usePermissions();
  const canEdit = can("apps:write") && can("connections:read");
  const current = app.default_connection;
  return (
    <div className="grid gap-3">
      {canEdit ? (
        <DefaultConnectionSelect app={app} />
      ) : current ? (
        <ConnectionLink connection={current} />
      ) : (
        <span className="text-muted-foreground">{strings.common.notSet}</span>
      )}
      {!current && (
        <Alert variant="warning">
          <TriangleAlertIcon aria-hidden="true" />
          <AlertDescription>{strings.apps.defaultConnectionUnset}</AlertDescription>
        </Alert>
      )}
    </div>
  );
}

function DefaultConnectionSelect({ app }: { app: App }) {
  const options = useConnectionOptions(app.environment);
  const update = useUpdateApp();
  const [fieldError, setFieldError] = useState<string | null>(null);
  const triggerId = useId();
  const messageId = useId();
  const current = app.default_connection;

  if (options.isPending) return <Skeleton className="h-9 w-full max-w-sm" data-testid="default-connection-skeleton" />;
  if (options.isError) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-danger-foreground">{strings.apps.defaultConnectionLoadFailed}</span>
        <Button variant="outline" size="sm" onClick={() => void options.refetch()}>
          <RotateCwIcon aria-hidden="true" />
          {strings.common.retry}
        </Button>
      </div>
    );
  }

  // The current default is always active, but it may sit past the first page.
  const choices: ConnectionSummary[] = [...options.data];
  if (current && !choices.some((c) => c.id === current.id)) choices.unshift(current);

  if (choices.length === 0) {
    return (
      <p className="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
        <span className="text-muted-foreground">{strings.apps.defaultConnectionNoOptions(app.environment)}</span>
        <Link
          to={`/connections/new?environment=${app.environment}`}
          className="rounded-sm text-link underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:inline-flex max-md:min-h-11 max-md:items-center"
        >
          {strings.apps.defaultConnectionAdd}
        </Link>
      </p>
    );
  }

  function change(value: string) {
    const id = value === NONE ? null : value;
    if (id === (current?.id ?? null)) return;
    setFieldError(null);
    update.mutate(
      { id: app.id, body: { default_connection_id: id } },
      {
        onSuccess: (result) => toast.success(strings.apps.defaultConnectionUpdated(result.app.name)),
        onError: (error) => {
          const message = fieldMessage(error);
          if (message) setFieldError(message);
          else toast.error(errorMessage(error, strings.apps.defaultConnectionFailed));
        },
      },
    );
  }

  return (
    <div className="grid max-w-sm gap-2">
      <label htmlFor={triggerId} className="sr-only">
        {strings.apps.defaultConnection}
      </label>
      <div className="flex items-center gap-2">
        <Select value={current?.id ?? NONE} onValueChange={change} disabled={update.isPending}>
          <SelectTrigger
            id={triggerId}
            aria-invalid={fieldError !== null}
            aria-describedby={fieldError ? messageId : undefined}
            className="font-mono"
          >
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value={NONE}>{strings.apps.defaultConnectionNone}</SelectItem>
            {choices.map((connection) => (
              <SelectItem key={connection.id} value={connection.id} className="font-mono">
                {connection.name}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {update.isPending && (
          <span className="flex shrink-0 items-center gap-1 text-xs text-muted-foreground" role="status">
            <Loader2Icon className="size-3.5 animate-spin" aria-hidden="true" />
            {strings.apps.defaultConnectionSaving}
          </span>
        )}
      </div>
      {fieldError && (
        <p id={messageId} className="text-xs font-medium text-danger-foreground">
          {fieldError}
        </p>
      )}
    </div>
  );
}
