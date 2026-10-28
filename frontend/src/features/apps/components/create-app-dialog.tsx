import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useId, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { errorMessage } from "@/shared/api/error-message";
import { useMode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { RadioGroup, RadioGroupItem } from "@/shared/ui/radio-group";
import { APP_ENVIRONMENTS, useCreateApp, type AppCreated, type CreateAppRequest } from "../api/apps";
import { createAppSchema, suggestSlug, type CreateAppValues } from "../schemas/app-form";
import { parseMetadataText } from "../schemas/metadata";
import { applyServerErrors } from "./apply-server-errors";
import { MetadataEditor } from "./metadata-editor";
import { UrlFields } from "./url-fields";

const FIELD_NAMES = [
  "name",
  "slug",
  "environment",
  "webhook_url",
  "redirect_success_url",
  "redirect_failure_url",
  "metadata",
] as const;

interface CreateAppDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (result: AppCreated) => void;
}

export function CreateAppDialog({ open, onOpenChange, onCreated }: CreateAppDialogProps) {
  const create = useCreateApp();
  return (
    <Dialog open={open} onOpenChange={(next) => !create.isPending && onOpenChange(next)}>
      {open && <CreateAppForm create={create} onCreated={onCreated} onCancel={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function toRequest(values: CreateAppValues): { body: CreateAppRequest; metadataText?: string } {
  const body: CreateAppRequest = {
    name: values.name,
    slug: values.slug,
    environment: values.environment,
    webhook_url: values.webhook_url,
    redirect_success_url: values.redirect_success_url,
    redirect_failure_url: values.redirect_failure_url,
  };
  const metadata = parseMetadataText(values.metadata);
  if (!metadata.ok || Object.keys(metadata.value).length === 0) return { body };
  return { body: { ...body, metadata: metadata.value }, metadataText: values.metadata };
}

function CreateAppForm({
  create,
  onCreated,
  onCancel,
}: {
  create: ReturnType<typeof useCreateApp>;
  onCreated: CreateAppDialogProps["onCreated"];
  onCancel: () => void;
}) {
  const { mode } = useMode();
  const [formError, setFormError] = useState<string | null>(null);
  // The slug follows the name until the user types in the slug field.
  const slugEdited = useRef(false);
  const environmentLabelId = useId();
  const form = useForm<CreateAppValues>({
    resolver: zodResolver(createAppSchema),
    defaultValues: {
      name: "",
      slug: "",
      environment: mode,
      webhook_url: "",
      redirect_success_url: "",
      redirect_failure_url: "",
      metadata: "",
    },
  });
  const pending = create.isPending;

  function onSubmit(values: CreateAppValues) {
    setFormError(null);
    create.mutate(toRequest(values), {
      onSuccess: onCreated,
      onError: (error) => {
        const placed = applyServerErrors(error, form.setError, FIELD_NAMES, {
          slugConflict: strings.apps.slugTaken(values.environment),
        });
        if (!placed) setFormError(errorMessage(error, strings.apps.createFailed));
      },
    });
  }

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{strings.apps.createTitle}</DialogTitle>
        <DialogDescription>{strings.apps.createDescription}</DialogDescription>
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
                    <Input
                      autoComplete="off"
                      placeholder={strings.apps.namePlaceholder}
                      {...field}
                      onChange={(event) => {
                        field.onChange(event);
                        if (!slugEdited.current) form.setValue("slug", suggestSlug(event.target.value));
                      }}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="slug"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.apps.slug}</FormLabel>
                  <FormControl>
                    <Input
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      placeholder="acme-store"
                      className="font-mono"
                      {...field}
                      onChange={(event) => {
                        slugEdited.current = true;
                        field.onChange(event);
                      }}
                    />
                  </FormControl>
                  <FormDescription>{strings.apps.slugHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="environment"
              render={({ field }) => (
                <FormItem>
                  <FormLabel id={environmentLabelId}>{strings.apps.environment}</FormLabel>
                  <FormControl>
                    <RadioGroup
                      aria-labelledby={environmentLabelId}
                      value={field.value}
                      onValueChange={field.onChange}
                      disabled={pending}
                      className="flex flex-wrap gap-x-6 gap-y-2"
                    >
                      {APP_ENVIRONMENTS.map((environment) => {
                        const itemId = `${environmentLabelId}-${environment}`;
                        return (
                          <div key={environment} className="flex items-center gap-2 max-md:min-h-11">
                            <RadioGroupItem id={itemId} value={environment} />
                            <Label htmlFor={itemId}>{strings.environments[environment]}</Label>
                          </div>
                        );
                      })}
                    </RadioGroup>
                  </FormControl>
                  <FormDescription>{strings.apps.environmentHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <UrlFields control={form.control} />
            <MetadataEditor control={form.control} />
          </fieldset>
          <DialogFooter>
            <Button variant="outline" onClick={onCancel} disabled={pending}>
              {strings.common.cancel}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
              {pending ? strings.apps.creating : strings.apps.create}
            </Button>
          </DialogFooter>
        </form>
      </Form>
    </DialogContent>
  );
}
