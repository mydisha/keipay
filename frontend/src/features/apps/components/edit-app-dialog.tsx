import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { useUpdateTenant, type Tenant } from "../api/tenants";
import { editAppSchema, type EditAppValues } from "../schemas/app-form";
import { applyServerErrors } from "./apply-server-errors";
import { UrlFields } from "./url-fields";

const FIELD_NAMES = ["name", "webhook_url", "redirect_success_url", "redirect_failure_url", "active"] as const;

interface EditAppDialogProps {
  tenant: Tenant | null;
  onClose: () => void;
}

export function EditAppDialog({ tenant, onClose }: EditAppDialogProps) {
  const update = useUpdateTenant();
  return (
    <Dialog open={tenant !== null} onOpenChange={(open) => !open && !update.isPending && onClose()}>
      {tenant && <EditAppForm key={tenant.id} tenant={tenant} update={update} onClose={onClose} />}
    </Dialog>
  );
}

function EditAppForm({
  tenant,
  update,
  onClose,
}: {
  tenant: Tenant;
  update: ReturnType<typeof useUpdateTenant>;
  onClose: () => void;
}) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<EditAppValues>({
    resolver: zodResolver(editAppSchema),
    defaultValues: {
      name: tenant.name,
      webhook_url: tenant.webhook_url,
      redirect_success_url: tenant.redirect_success_url,
      redirect_failure_url: tenant.redirect_failure_url,
      active: tenant.active,
    },
  });
  const pending = update.isPending;

  function onSubmit(values: EditAppValues) {
    setFormError(null);
    update.mutate(
      { id: tenant.id, body: values },
      {
        onSuccess: (result) => {
          toast.success(strings.apps.updated_toast(result.tenant.name));
          onClose();
        },
        onError: (error) => {
          if (!applyServerErrors(error, form.setError, FIELD_NAMES)) {
            setFormError(errorMessage(error, strings.apps.updateFailed));
          }
        },
      },
    );
  }

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{strings.apps.editTitle(tenant.name)}</DialogTitle>
        <DialogDescription>
          {strings.apps.editDescription} <span className="font-mono">{tenant.slug}</span>
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
            <FormField
              control={form.control}
              name="active"
              render={({ field }) => (
                <FormItem className="grid-cols-[auto_1fr] items-center gap-x-2 gap-y-1">
                  <FormControl>
                    <Checkbox checked={field.value} onCheckedChange={(v) => field.onChange(v === true)} />
                  </FormControl>
                  <FormLabel>{strings.apps.active}</FormLabel>
                  <FormDescription className="col-start-2">{strings.apps.activeHint}</FormDescription>
                </FormItem>
              )}
            />
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
