import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, KeyRoundIcon, Loader2Icon } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { z } from "zod";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { DateTimeText } from "@/shared/ui/date-time-text";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/shared/ui/dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { IdText } from "@/shared/ui/id-text";
import { Input } from "@/shared/ui/input";
import {
  useUpdateConnection,
  type Connection,
  type Provider,
  type UpdateConnectionRequest,
} from "../api/connections";
import { applyConnectionErrors, connectionNameSchema, normalizeName } from "../schemas/connection-form";
import { hasUnsupportedField, schemaDefaults, schemaFormSchema, schemaPayload, type SchemaValues } from "../schemas/schema-form";
import { LastTest } from "./last-test";
import { ReplaceCredentialsForm } from "./replace-credentials-form";
import { SchemaFields } from "./schema-fields";

export function Section({
  id,
  title,
  description,
  action,
  children,
}: {
  id: string;
  title: string;
  description?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  const headingId = `connection-${id}-heading`;
  return (
    <section className="grid gap-3" aria-labelledby={headingId}>
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div className="grid gap-1">
          <h2 id={headingId} className="text-base font-semibold">
            {title}
          </h2>
          {description && <p className="text-sm text-muted-foreground">{description}</p>}
        </div>
        {action}
      </div>
      {children}
    </section>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid min-w-0 gap-1 sm:grid-cols-[12rem_minmax(0,1fr)] sm:gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

/** Id, provider, times and the last test with its message. */
export function ConnectionFacts({ connection, providerName }: { connection: Connection; providerName: string }) {
  return (
    <Card className="p-4">
      <dl className="grid gap-3 text-sm">
        <Fact label={strings.connections.connectionId}>
          <IdText value={connection.id} label={strings.connections.connectionId} />
        </Fact>
        <Fact label={strings.connections.provider}>{providerName}</Fact>
        <Fact label={strings.connections.created}>
          <DateTimeText value={connection.created_at} />
        </Fact>
        <Fact label={strings.connections.updated}>
          <DateTimeText value={connection.updated_at} />
        </Fact>
        <Fact label={strings.connections.lastTest}>
          <div className="grid gap-1">
            <LastTest connection={connection} />
            {connection.last_test_status === "failed" && connection.last_test_error && (
              <p className="text-sm">{connection.last_test_error}</p>
            )}
          </div>
        </Fact>
      </dl>
    </Card>
  );
}

/** One row per credential field: its label and `•••• last4` or `Stored`, never the value. */
export function CredentialHints({
  connection,
  provider,
  canWrite,
}: {
  connection: Connection;
  provider: Provider | undefined;
  canWrite: boolean;
}) {
  const [replacing, setReplacing] = useState(false);
  const fields = provider?.credential_schema.fields ?? [];
  const labelOf = (key: string) => fields.find((f) => f.key === key)?.label ?? key;

  return (
    <Section
      id="credentials"
      title={strings.connections.credentialsTitle}
      description={strings.connections.credentialsSectionBody}
      action={
        canWrite && (
          <Button variant="outline" onClick={() => setReplacing(true)}>
            <KeyRoundIcon aria-hidden="true" />
            {strings.connections.replaceCredentials}
          </Button>
        )
      }
    >
      <Card className="p-4">
        <dl className="grid gap-3 text-sm">
          {connection.credentials.map((hint) => (
            <Fact key={hint.key} label={labelOf(hint.key)}>
              <span className="font-mono text-[0.8125rem]">
                {hint.last4 ? strings.connections.maskedHint(hint.last4) : strings.connections.stored}
              </span>
            </Fact>
          ))}
        </dl>
      </Card>
      <Dialog open={replacing} onOpenChange={setReplacing}>
        {replacing && (
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{strings.connections.replaceTitle}</DialogTitle>
              <DialogDescription>{strings.connections.replaceBody}</DialogDescription>
            </DialogHeader>
            <ReplaceCredentialsForm
              connection={connection}
              fields={fields}
              submitLabel={strings.connections.replaceSubmit}
              secondaryActions={
                <Button variant="outline" onClick={() => setReplacing(false)}>
                  {strings.common.cancel}
                </Button>
              }
              renderActions={(actions) => <DialogFooter>{actions}</DialogFooter>}
              onSaved={() => {
                setReplacing(false);
                toast.success(strings.connections.replaced_toast);
              }}
            />
          </DialogContent>
        )}
      </Dialog>
    </Section>
  );
}

interface DetailsValues {
  name: string;
  settings: SchemaValues;
}

/** Name and provider settings; saving sends only what changed. Read-only without `connections:write`. */
export function DetailsForm({
  connection,
  provider,
  readOnly,
}: {
  connection: Connection;
  provider: Provider | undefined;
  readOnly: boolean;
}) {
  const update = useUpdateConnection();
  const [formError, setFormError] = useState<string | null>(null);
  const settingsFields = useMemo(() => provider?.settings_schema.fields ?? [], [provider]);
  const schema = useMemo(
    () => z.object({ name: connectionNameSchema, settings: schemaFormSchema(settingsFields) }),
    [settingsFields],
  );
  const form = useForm<DetailsValues>({
    resolver: zodResolver(schema),
    defaultValues: { name: connection.name, settings: schemaDefaults(settingsFields, connection.settings) },
  });
  const pending = update.isPending;
  const unsupported = hasUnsupportedField(settingsFields);

  function changes(values: DetailsValues): UpdateConnectionRequest {
    const body: UpdateConnectionRequest = {};
    const name = normalizeName(values.name);
    if (name !== connection.name) body.name = name;
    const settings = schemaPayload(settingsFields, values.settings);
    const current = schemaPayload(settingsFields, schemaDefaults(settingsFields, connection.settings));
    if (JSON.stringify(settings) !== JSON.stringify(current)) body.settings = settings;
    return body;
  }

  function onSubmit(values: DetailsValues) {
    setFormError(null);
    const body = changes(values);
    if (Object.keys(body).length === 0) {
      toast.info(strings.connections.nothingChanged);
      return;
    }
    update.mutate(
      { id: connection.id, body },
      {
        onSuccess: (result) => {
          toast.success(strings.connections.updated_toast(result.connection.name));
          form.reset({
            name: result.connection.name,
            settings: schemaDefaults(settingsFields, result.connection.settings),
          });
        },
        onError: (error) => {
          const placed = applyConnectionErrors(error, form.setError, {
            fieldNames: ["name"],
            sections: { settings: settingsFields },
          });
          if (!placed.complete) setFormError(errorMessage(error, strings.connections.updateFailed));
        },
      },
    );
  }

  return (
    <Card className="p-4">
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-5">
          {formError && (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          <fieldset disabled={pending || readOnly} className="grid gap-5">
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.connections.name}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" autoCapitalize="off" spellCheck={false} className="font-mono" {...field} />
                  </FormControl>
                  <FormDescription>{strings.connections.nameHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <div className="grid gap-4 border-t pt-4">
              <h3 className="text-sm font-semibold">{strings.connections.settingsTitle}</h3>
              {settingsFields.length === 0 ? (
                <p className="text-sm text-muted-foreground">{strings.connections.noSettings}</p>
              ) : (
                <SchemaFields
                  control={form.control}
                  prefix="settings"
                  fields={settingsFields}
                  disabled={pending || readOnly}
                />
              )}
            </div>
          </fieldset>
          {!readOnly && (
            <div className="flex justify-end">
              <Button type="submit" disabled={pending || unsupported}>
                {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
                {pending ? strings.common.saving : strings.connections.saveDetails}
              </Button>
            </div>
          )}
        </form>
      </Form>
    </Card>
  );
}
