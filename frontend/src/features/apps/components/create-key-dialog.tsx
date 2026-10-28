import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { useCreateApiKey, type ApiKeyCreated } from "../api/apps";
import { createKeySchema, wibInputToDate, type CreateKeyValues } from "../schemas/key-form";
import { applyServerErrors } from "./apply-server-errors";

const FIELD_NAMES = ["label", "expires_at"] as const;

interface CreateKeyDialogProps {
  appId: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (result: ApiKeyCreated) => void;
}

export function CreateKeyDialog({ appId, open, onOpenChange, onCreated }: CreateKeyDialogProps) {
  const create = useCreateApiKey();
  return (
    <Dialog open={open} onOpenChange={(next) => !create.isPending && onOpenChange(next)}>
      {open && (
        <CreateKeyForm appId={appId} create={create} onCreated={onCreated} onCancel={() => onOpenChange(false)} />
      )}
    </Dialog>
  );
}

function CreateKeyForm({
  appId,
  create,
  onCreated,
  onCancel,
}: {
  appId: string;
  create: ReturnType<typeof useCreateApiKey>;
  onCreated: CreateKeyDialogProps["onCreated"];
  onCancel: () => void;
}) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<CreateKeyValues>({
    resolver: zodResolver(createKeySchema),
    defaultValues: { label: "", expires_at: "" },
  });
  const pending = create.isPending;

  function onSubmit(values: CreateKeyValues) {
    setFormError(null);
    const expires = values.expires_at ? wibInputToDate(values.expires_at) : null;
    create.mutate(
      { appId, body: { label: values.label, expires_at: expires ? expires.toISOString() : null } },
      {
        onSuccess: onCreated,
        onError: (error) => {
          // The 10-key limit: nothing in the form can fix it.
          if (isApiError(error) && error.code === "validation_failed") {
            toast.error(errorMessage(error, strings.keys.createFailed));
            onCancel();
            return;
          }
          if (!applyServerErrors(error, form.setError, FIELD_NAMES)) {
            setFormError(errorMessage(error, strings.keys.createFailed));
          }
        },
      },
    );
  }

  return (
    <DialogContent showCloseButton={!pending}>
      <DialogHeader>
        <DialogTitle>{strings.keys.createTitle}</DialogTitle>
        <DialogDescription>{strings.keys.createDescription}</DialogDescription>
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
              name="label"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.keys.label}</FormLabel>
                  <FormControl>
                    <Input autoComplete="off" placeholder={strings.keys.labelPlaceholder} {...field} />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />
            <FormField
              control={form.control}
              name="expires_at"
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{strings.keys.expiresAt}</FormLabel>
                  <FormControl>
                    <Input type="datetime-local" {...field} />
                  </FormControl>
                  <FormDescription>{strings.keys.expiresHint}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </fieldset>
          <DialogFooter>
            <Button variant="outline" onClick={onCancel} disabled={pending}>
              {strings.common.cancel}
            </Button>
            <Button type="submit" disabled={pending}>
              {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
              {pending ? strings.keys.creating : strings.keys.create}
            </Button>
          </DialogFooter>
        </form>
      </Form>
    </DialogContent>
  );
}
