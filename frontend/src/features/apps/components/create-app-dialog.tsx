import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { useCreateTenant, type Tenant } from "../api/tenants";
import { createAppSchema, type CreateAppValues } from "../schemas/app-form";
import type { components } from "@/shared/api/schema";
import { applyServerErrors } from "./apply-server-errors";
import { UrlFields } from "./url-fields";

type Credentials = components["schemas"]["TenantCredentials"];

const FIELD_NAMES = ["name", "slug", "webhook_url", "redirect_success_url", "redirect_failure_url"] as const;

interface CreateAppDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (tenant: Tenant, credentials: Credentials) => void;
}

export function CreateAppDialog({ open, onOpenChange, onCreated }: CreateAppDialogProps) {
  const create = useCreateTenant();
  return (
    <Dialog open={open} onOpenChange={(next) => !create.isPending && onOpenChange(next)}>
      {open && <CreateAppForm create={create} onCreated={onCreated} onCancel={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function CreateAppForm({
  create,
  onCreated,
  onCancel,
}: {
  create: ReturnType<typeof useCreateTenant>;
  onCreated: CreateAppDialogProps["onCreated"];
  onCancel: () => void;
}) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<CreateAppValues>({
    resolver: zodResolver(createAppSchema),
    defaultValues: { name: "", slug: "", webhook_url: "", redirect_success_url: "", redirect_failure_url: "" },
  });
  const pending = create.isPending;

  function onSubmit(values: CreateAppValues) {
    setFormError(null);
    create.mutate(values, {
      onSuccess: (result) => onCreated(result.tenant, result.credentials),
      onError: (error) => {
        if (!applyServerErrors(error, form.setError, FIELD_NAMES)) {
          setFormError(errorMessage(error, strings.apps.createFailed));
        }
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
                    <Input autoComplete="off" placeholder="Acme Store" {...field} />
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
                    <Input autoComplete="off" autoCapitalize="off" spellCheck={false} placeholder="acme" className="font-mono" {...field} />
                  </FormControl>
                  <FormDescription>{strings.apps.slugHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
            <UrlFields control={form.control} />
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
