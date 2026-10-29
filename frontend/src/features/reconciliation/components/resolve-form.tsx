import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useState } from "react";
import { useForm } from "react-hook-form";
import { toast } from "sonner";
import { errorMessage } from "@/shared/api/error-message";
import { isApiError } from "@/shared/api/errors";
import { applyFieldErrors } from "@/shared/lib/field-errors";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form, FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Textarea } from "@/shared/ui/textarea";
import type { ReconciliationItem, ResolveItemMutation } from "../api/runs";
import { MAX_NOTE_LENGTH, noteLength, resolveFormSchema, resolveRuleMessages, type ResolveFormValues } from "../schemas/resolve-form";

/** The message for a refused resolve that has no field to go on. */
function resolveErrorMessage(error: unknown): string {
  if (isApiError(error)) {
    if (error.code === "conflict") return strings.reconciliation.alreadyResolved;
    if (error.code === "run_in_progress") return strings.reconciliation.runInProgress;
    if (error.code === "run_not_succeeded") return strings.reconciliation.runNotSucceeded;
    if (error.code === "validation_failed") return strings.reconciliation.matchedNotResolvable;
  }
  return errorMessage(error, strings.reconciliation.resolveFailed);
}

/**
 * A resolve swaps this form for the resolution as soon as the item is written,
 * so the mutation is passed in by a parent that outlives it: its callbacks and
 * pending state would otherwise go with the unmounted form.
 */
export function ResolveForm({ item, resolve }: { item: ReconciliationItem; resolve: ResolveItemMutation }) {
  const [formError, setFormError] = useState<string | null>(null);
  const form = useForm<ResolveFormValues>({ resolver: zodResolver(resolveFormSchema), defaultValues: { note: "" } });
  const pending = resolve.isPending;
  const length = noteLength(form.watch("note"));

  function onSubmit(values: ResolveFormValues) {
    setFormError(null);
    resolve.mutate(
      { id: item.id, note: values.note.trim() },
      {
        onSuccess: () => toast.success(strings.reconciliation.resolved_toast),
        onError: (error) => {
          if (!applyFieldErrors(error, form.setError, ["note"], resolveRuleMessages)) setFormError(resolveErrorMessage(error));
        },
      },
    );
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-3">
        {formError && (
          <Alert variant="danger">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription>{formError}</AlertDescription>
          </Alert>
        )}
        <FormField
          control={form.control}
          name="note"
          render={({ field }) => (
            <FormItem>
              <div className="flex items-baseline justify-between gap-2">
                <FormLabel>{strings.reconciliation.noteLabel}</FormLabel>
                <span
                  className={length > MAX_NOTE_LENGTH ? "text-xs text-danger-foreground tabular-nums" : "text-xs text-muted-foreground tabular-nums"}
                  aria-hidden="true"
                >
                  {strings.reconciliation.noteCounter(length, MAX_NOTE_LENGTH)}
                </span>
              </div>
              <FormControl>
                <Textarea rows={4} disabled={pending} {...field} />
              </FormControl>
              <FormDescription>{strings.reconciliation.noteHint}</FormDescription>
              <FormMessage />
            </FormItem>
          )}
        />
        <div className="flex justify-end">
          <Button type="submit" disabled={pending}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.reconciliation.resolving : strings.reconciliation.resolve}
          </Button>
        </div>
      </form>
    </Form>
  );
}
