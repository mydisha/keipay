import { zodResolver } from "@hookform/resolvers/zod";
import { Loader2Icon, PlugIcon } from "lucide-react";
import { useState } from "react";
import { useForm, type Control } from "react-hook-form";
import { Link, useNavigate } from "react-router";
import { toast } from "sonner";
import {
  useConnectionFilterOptions,
  useProviders,
  type ConnectionListItem,
  type Provider,
} from "@/features/connections/api/connections";
import { errorMessage } from "@/shared/api/error-message";
import { applyFieldErrors } from "@/shared/lib/field-errors";
import { useMode, type Mode } from "@/shared/lib/mode";
import { strings } from "@/shared/lib/strings";
import { addDays, wibDate } from "@/shared/lib/wib-dates";
import { Button } from "@/shared/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/shared/ui/dialog";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { Form, FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Skeleton } from "@/shared/ui/skeleton";
import { useCreateRun } from "../api/runs";
import { runPath } from "../lib/reconciliation-search";
import { runFormSchema, runRuleMessages, toCreateRunRequest, type RunFormValues } from "../schemas/run-form";

const FIELD_NAMES = ["connection_id", "from", "to"] as const;

/** Connections of the mode whose provider can list its transactions; disabled ones included. */
export function eligibleConnections(
  connections: ConnectionListItem[],
  providers: Provider[],
  environment: Mode,
): ConnectionListItem[] {
  const listable = new Set(providers.filter((p) => p.capabilities.list_transactions).map((p) => p.name));
  return connections.filter((c) => c.environment === environment && listable.has(c.provider));
}

interface RunDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

export function RunDialog({ open, onOpenChange }: RunDialogProps) {
  const create = useCreateRun();
  return (
    <Dialog open={open} onOpenChange={(next) => !create.isPending && onOpenChange(next)}>
      {open && <RunDialogBody create={create} onCancel={() => onOpenChange(false)} />}
    </Dialog>
  );
}

function RunDialogBody({ create, onCancel }: { create: ReturnType<typeof useCreateRun>; onCancel: () => void }) {
  const { mode } = useMode();
  const connections = useConnectionFilterOptions();
  const providers = useProviders();

  let body;
  if (connections.isPending || providers.isPending) {
    body = (
      <div className="grid gap-3" aria-busy="true">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-2/3" />
      </div>
    );
  } else if (connections.isError || providers.isError) {
    body = (
      <ErrorState
        title={strings.reconciliation.optionsFailed}
        error={connections.error ?? providers.error}
        onRetry={() => {
          if (connections.isError) void connections.refetch();
          if (providers.isError) void providers.refetch();
        }}
      />
    );
  } else {
    const options = eligibleConnections(connections.data, providers.data, mode);
    body =
      options.length === 0 ? (
        <NoEligibleConnection mode={mode} onCancel={onCancel} />
      ) : (
        <RunForm options={options} create={create} onCancel={onCancel} />
      );
  }

  return (
    <DialogContent showCloseButton={!create.isPending}>
      <DialogHeader>
        <DialogTitle>{strings.reconciliation.dialogTitle}</DialogTitle>
        <DialogDescription>{strings.reconciliation.dialogDescription}</DialogDescription>
      </DialogHeader>
      {body}
    </DialogContent>
  );
}

function NoEligibleConnection({ mode, onCancel }: { mode: Mode; onCancel: () => void }) {
  return (
    <EmptyState
      icon={PlugIcon}
      headingLevel={3}
      className="py-8"
      title={strings.reconciliation.noEligibleTitle}
      description={strings.reconciliation.noEligibleBody(strings.environments[mode]?.toLowerCase() ?? mode)}
      action={
        <>
          <Button variant="outline" onClick={onCancel}>
            {strings.common.cancel}
          </Button>
          <Button asChild variant="outline">
            <Link to="/connections">{strings.reconciliation.viewConnections}</Link>
          </Button>
        </>
      }
    />
  );
}

function RunForm({
  options,
  create,
  onCancel,
}: {
  options: ConnectionListItem[];
  create: ReturnType<typeof useCreateRun>;
  onCancel: () => void;
}) {
  const navigate = useNavigate();
  const [today] = useState(() => wibDate(new Date()));
  const yesterday = addDays(today, -1);
  const form = useForm<RunFormValues>({
    resolver: zodResolver(runFormSchema(today)),
    defaultValues: { connection_id: options.length === 1 ? (options[0]?.id ?? "") : "", from: yesterday, to: yesterday },
  });
  const pending = create.isPending;

  function onSubmit(values: RunFormValues) {
    create.mutate(toCreateRunRequest(values), {
      onSuccess: (run) => {
        toast.success(strings.reconciliation.started_toast);
        void navigate(runPath(run.id));
      },
      onError: (error) => {
        if (!applyFieldErrors(error, form.setError, FIELD_NAMES, runRuleMessages)) {
          toast.error(errorMessage(error, strings.reconciliation.startFailed));
        }
      },
    });
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-4">
        <fieldset disabled={pending} className="grid gap-4">
          <FormField
            control={form.control}
            name="connection_id"
            render={({ field }) => (
              <FormItem>
                <FormLabel>{strings.reconciliation.connectionLabel}</FormLabel>
                <Select value={field.value} onValueChange={field.onChange} disabled={pending}>
                  <FormControl>
                    <SelectTrigger onBlur={field.onBlur} ref={field.ref}>
                      <SelectValue placeholder={strings.reconciliation.connectionPlaceholder} />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent>
                    {options.map((c) => {
                      const label = c.status === "disabled" ? strings.reconciliation.connectionDisabled(c.name) : c.name;
                      return (
                        <SelectItem key={c.id} value={c.id} textValue={label}>
                          {label}
                        </SelectItem>
                      );
                    })}
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          <div className="grid gap-4 sm:grid-cols-2">
            <DateField control={form.control} name="from" label={strings.reconciliation.from} />
            <DateField control={form.control} name="to" label={strings.reconciliation.to} />
          </div>
          <p className="text-xs text-muted-foreground">{strings.reconciliation.rangeHint}</p>
        </fieldset>
        <DialogFooter>
          <Button variant="outline" onClick={onCancel} disabled={pending}>
            {strings.common.cancel}
          </Button>
          <Button type="submit" disabled={pending}>
            {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
            {pending ? strings.reconciliation.starting : strings.reconciliation.start}
          </Button>
        </DialogFooter>
      </form>
    </Form>
  );
}

function DateField({
  control,
  name,
  label,
}: {
  control: Control<RunFormValues>;
  name: "from" | "to";
  label: string;
}) {
  return (
    <FormField
      control={control}
      name={name}
      render={({ field }) => (
        <FormItem>
          <FormLabel>{label}</FormLabel>
          <FormControl>
            <Input type="date" {...field} />
          </FormControl>
          <FormMessage />
        </FormItem>
      )}
    />
  );
}
