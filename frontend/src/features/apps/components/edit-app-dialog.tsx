import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { useUpdateApp, type App, type Metadata, type UpdateAppRequest } from "../api/apps";
import { editAppSchema, type EditAppValues } from "../schemas/app-form";
import { metadataToText, parseMetadataText } from "../schemas/metadata";
import { applyServerErrors, unplacedErrorMessage } from "./apply-server-errors";
import { MetadataEditor } from "./metadata-editor";
import { UrlFields } from "./url-fields";

const FIELD_NAMES = ["name", "redirect_success_url", "redirect_failure_url", "metadata"] as const;
const TEXT_FIELDS = ["name", "redirect_success_url", "redirect_failure_url"] as const;

interface EditAppDialogProps {
  app: App | null;
  onClose: () => void;
}

export function EditAppDialog({ app, onClose }: EditAppDialogProps) {
  const update = useUpdateApp();
  return (
    <Dialog open={app !== null} onOpenChange={(open) => !open && !update.isPending && onClose()}>
      {app && <EditAppForm key={app.id} app={app} update={update} onClose={onClose} />}
    </Dialog>
  );
}

function sameMetadata(a: Metadata, b: Metadata): boolean {
  const keys = Object.keys(a);
  return keys.length === Object.keys(b).length && keys.every((key) => key in b && Object.is(a[key], b[key]));
}

/** Only the fields that differ from the app: PATCH semantics, and the audit log records just those. */
function changedFields(app: App, values: EditAppValues): { body: UpdateAppRequest; metadataText?: string } {
  const body: UpdateAppRequest = {};
  for (const field of TEXT_FIELDS) {
    if (values[field] !== app[field]) body[field] = values[field];
  }
  const metadata = parseMetadataText(values.metadata);
  if (metadata.ok && !sameMetadata(metadata.value, app.metadata)) {
    body.metadata = metadata.value;
    return { body, metadataText: values.metadata };
  }
  return { body };
}

function EditAppForm({ app, update, onClose }: { app: App; update: ReturnType<typeof useUpdateApp>; onClose: () => void }) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<EditAppValues>({
    resolver: zodResolver(editAppSchema),
    defaultValues: {
      name: app.name,
      redirect_success_url: app.redirect_success_url,
      redirect_failure_url: app.redirect_failure_url,
      metadata: metadataToText(app.metadata),
    },
  });
  const pending = update.isPending;

  function onSubmit(values: EditAppValues) {
    setFormError(null);
    const { body, metadataText } = changedFields(app, values);
    if (Object.keys(body).length === 0) {
      onClose();
      return;
    }
    update.mutate(
      { id: app.id, body, metadataText },
      {
        onSuccess: (result) => {
          toast.success(strings.apps.updated_toast(result.app.name));
          onClose();
        },
        onError: (error) => {
          if (!applyServerErrors(error, form.setError, FIELD_NAMES)) {
            setFormError(unplacedErrorMessage(error, strings.apps.updateFailed));
          }
        },
      },
    );
  }

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{strings.apps.editTitle(app.name)}</DialogTitle>
        <DialogDescription>
          {strings.apps.editDescription}
        </DialogDescription>
      </DialogHeader>
      <Form {...form}>
        <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-4">
          {formError && (
            <Alert variant="danger">
              <CircleAlertIcon aria-hidden="true" />
              <AlertDescription>{formError}</AlertDescription>
            </Alert>
          )}
          <fieldset disabled={pending} className="grid gap-4">
            <FormField
              control={form.control}
              name="name"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.apps.name}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <UrlFields control={form.control} />
            <MetadataEditor control={form.control} label={strings.apps.metadata} />
          </fieldset>
          <DialogFooter>
            <Button variant="outline" onClick={onClose} disabled={pending}>
              {strings.common.cancel}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
              {pending ? strings.common.saving : strings.common.save}
            </Button>
          </DialogFooter>
        </form>
      </Form>
    </DialogContent>
  );
}
