import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import type { App } from "@/features/apps/api/apps";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { SidePanel } from "@/shared/ui/side-panel";
import { Switch } from "@/shared/ui/switch";
import { Textarea } from "@/shared/ui/textarea";
import {
  WEBHOOK_API_VERSIONS,
  useCreateWebhookEndpoint,
  useUpdateWebhookEndpoint,
  type WebhookEndpoint,
  type WebhookEndpointSecret,
} from "../api/endpoints";
import { applyEndpointServerErrors } from "../schemas/endpoint-errors";
import {
  defaultEndpointValues,
  endpointFormSchema,
  toCreateRequest,
  toUpdateRequest,
  type EndpointFormValues,
} from "../schemas/endpoint-form";
import { EventsPicker } from "./events-picker";
import { HeadersEditor } from "./headers-editor";

const FORM_ID = "webhook-endpoint-form";

interface EndpointSheetProps {
  app: App;
  /** `{ endpoint: null }` adds one; an endpoint edits it; null closes the sheet. */
  target: { endpoint: WebhookEndpoint | null } | null;
  onClose: () => void;
  onCreated: (result: WebhookEndpointSecret) => void;
}

export function EndpointSheet({ app, target, onClose, onCreated }: EndpointSheetProps) {
  const create = useCreateWebhookEndpoint();
  const update = useUpdateWebhookEndpoint();
  const pending = create.isPending || update.isPending;
  const editing = target?.endpoint ?? null;

  return (
    <SidePanel
      open={target !== null}
      onOpenChange={(open) => !open && onClose()}
      locked={pending}
      title={editing ? strings.endpoints.editTitle : strings.endpoints.addTitle}
      description={strings.endpoints.sheetDescription}
      footer={
        <>
          <Button variant="outline" onClick={onClose} disabled={pending}>
            {strings.common.cancel}
          </Button>
          <Button type="submit" form={FORM_ID} disabled={pending}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {editing
              ? pending
                ? strings.endpoints.saving
                : strings.endpoints.save
              : pending
                ? strings.endpoints.adding
                : strings.endpoints.add}
          </Button>
        </>
      }
    >
      {target && (
        <EndpointForm
          key={editing?.id ?? "new"}
          app={app}
          endpoint={editing}
          pending={pending}
          onSubmit={(values, setFormError, form) => {
            const onError = (error: unknown) => {
              const placed = applyEndpointServerErrors(error, form.setError, values.replace_headers ? values.headers.length : 0);
              if (!placed) setFormError(errorMessage(error, editing ? strings.endpoints.updateFailed : strings.endpoints.createFailed));
            };
            if (!editing) {
              create.mutate(
                { appId: app.id, body: toCreateRequest(values) },
                {
                  onSuccess: (result) => {
                    toast.success(strings.endpoints.created_toast);
                    onCreated(result);
                  },
                  onError,
                },
              );
              return;
            }
            const body = toUpdateRequest(editing, values);
            if (Object.keys(body).length === 0) {
              onClose();
              return;
            }
            update.mutate(
              { id: editing.id, body },
              {
                onSuccess: () => {
                  toast.success(strings.endpoints.updated_toast);
                  onClose();
                },
                onError,
              },
            );
          }}
        />
      )}
    </SidePanel>
  );
}

type FormApi = ReturnType<typeof useForm<EndpointFormValues>>;

interface EndpointFormProps {
  app: App;
  endpoint: WebhookEndpoint | null;
  pending: boolean;
  onSubmit: (values: EndpointFormValues, setFormError: (message: string) => void, form: FormApi) => void;
}

function EndpointForm({ app, endpoint, pending, onSubmit }: EndpointFormProps) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<EndpointFormValues>({
    resolver: zodResolver(endpointFormSchema(app.environment)),
    defaultValues: defaultEndpointValues(endpoint),
  });

  return (
    <Form {...form}>
      <form
        id={FORM_ID}
        noValidate
        className="grid gap-5"
        onSubmit={form.handleSubmit((values) => {
          setFormError(null);
          onSubmit(values, setFormError, form);
        })}
      >
        {formError && (
          <Alert variant="danger">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription>{formError}</AlertDescription>
          </Alert>
        )}
        <fieldset disabled={pending} className="grid gap-5">
          <FormField
            control={form.control}
            name="url"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{strings.endpoints.url}</FormLabel>
                <FormControl>
                  <Input
                    type="url"
                    inputMode="url"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="https://shop.example.com/hooks/keipay"
                    className="font-mono"
                    {...field}
                  />
                </FormControl>
                <FormDescription>{strings.endpoints.urlHint}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="description"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{strings.endpoints.descriptionLabel}</FormLabel>
                <FormControl>
                  <Textarea rows={2} {...field} />
                </FormControl>
                <FormDescription>{strings.endpoints.descriptionHint}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="events"
            render={({ field, fieldState }) => (
              <EventsPicker
                value={field.value}
                onChange={field.onChange}
                disabled={pending}
                error={fieldState.error?.message}
              />
            )}
          />
          <FormField
            control={form.control}
            name="api_version"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{strings.endpoints.version}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange} disabled={pending}>
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {WEBHOOK_API_VERSIONS.map((version) => (
                      <SelectItem key={version} value={version}>
                        {strings.endpoints.versions[version] ?? version}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <FormDescription>{strings.endpoints.versionHint}</FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
          <FormField
            control={form.control}
            name="legacy_signature"
            render={({ field }) => (
              <FormItem>
                <div className="flex items-center gap-3 max-md:min-h-11">
                  <FormControl>
                    <Switch checked={field.value} onCheckedChange={field.onChange} disabled={pending} />
                  </FormControl>
                  <FormLabel>{strings.endpoints.legacySignatureLabel}</FormLabel>
                </div>
                <FormDescription>{strings.endpoints.legacySignatureHint}</FormDescription>
              </FormItem>
            )}
          />
          <HeadersEditor endpoint={endpoint} disabled={pending} />
          <RetryFields form={form} pending={pending} />
        </fieldset>
      </form>
    </Form>
  );
}

function RetryFields({ form, pending }: { form: FormApi; pending: boolean }) {
  return (
    <fieldset className="grid gap-3">
      <legend className="mb-1 text-sm font-medium">{strings.endpoints.retries}</legend>
      <p className="-mt-2 text-xs text-muted-foreground">{strings.endpoints.retriesHint}</p>
      <div className="grid gap-3 sm:grid-cols-2">
        <FormField
          control={form.control}
          name="retry_policy.max_attempts"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{strings.endpoints.maxAttempts}</FormLabel>
              <FormControl>
                <Input inputMode="numeric" autoComplete="off" className="tabular-nums" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
        <FormField
          control={form.control}
          name="retry_policy.interval_seconds"
          render={({ field }) => (
            <FormItem>
              <FormLabel>{strings.endpoints.intervalSeconds}</FormLabel>
              <FormControl>
                <Input inputMode="numeric" autoComplete="off" className="tabular-nums" {...field} />
              </FormControl>
              <FormMessage />
            </FormItem>
          )}
        />
      </div>
      <FormField
        control={form.control}
        name="retry_policy.backoff"
        render={({ field }) => (
          <FormItem>
            <FormLabel>{strings.endpoints.backoff}</FormLabel>
            <Select value={field.value} onValueChange={field.onChange} disabled={pending}>
              <FormControl>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
              </FormControl>
              <SelectContent>
                {(["exponential", "fixed"] as const).map((backoff) => (
                  <SelectItem key={backoff} value={backoff}>
                    {strings.endpoints.backoffs[backoff] ?? backoff}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <FormMessage />
          </FormItem>
        )}
      />
    </fieldset>
  );
}
