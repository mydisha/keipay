import { zodResolver } from "@hookform/resolvers/zod";
import { CircleAlertIcon, Loader2Icon } from "lucide-react";
import { useMemo, useState, type ReactNode } from "react";
import { useForm } from "react-hook-form";
import { z } from "zod";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Form } from "@/shared/ui/form";
import { useReplaceCredentials, type Connection, type SchemaField } from "../api/connections";
import { applyConnectionErrors } from "../schemas/connection-form";
import { credentialPayload, hasUnsupportedField, schemaDefaults, schemaFormSchema, type SchemaValues } from "../schemas/schema-form";
import { SchemaFields } from "./schema-fields";

interface ReplaceCredentialsFormProps {
  connection: Pick<Connection, "id" | "credentials">;
  fields: readonly SchemaField[];
  submitLabel: string;
  onSaved: (connection: Connection) => void;
  /** Buttons before submit, e.g. Cancel. */
  secondaryActions?: ReactNode;
  /** Wraps the action row (a dialog footer). */
  renderActions?: (actions: ReactNode) => ReactNode;
}

interface ReplaceValues {
  credentials: SchemaValues;
}

/** Credential fields in replace mode: blank keeps the stored value, and only filled keys are sent. */
export function ReplaceCredentialsForm({
  connection,
  fields,
  submitLabel,
  onSaved,
  secondaryActions,
  renderActions = (actions) => <div className="flex flex-wrap justify-end gap-2">{actions}</div>,
}: ReplaceCredentialsFormProps) {
  const replace = useReplaceCredentials();
  const [formError, setFormError] = useState<string | null>(null);
  const schema = useMemo(() => z.object({ credentials: schemaFormSchema(fields, "replace") }), [fields]);
  const form = useForm<ReplaceValues>({
    resolver: zodResolver(schema),
    defaultValues: { credentials: schemaDefaults(fields) },
  });
  const hints = Object.fromEntries(connection.credentials.map((hint) => [hint.key, hint.last4]));
  const unsupported = hasUnsupportedField(fields);
  const pending = replace.isPending;

  function onSubmit(values: ReplaceValues) {
    setFormError(null);
    replace.mutate(
      { id: connection.id, credentials: credentialPayload(fields, values.credentials) },
      {
        onSuccess: (result) => onSaved(result.connection),
        onError: (error) => {
          const placed = applyConnectionErrors(error, form.setError, { sections: { credentials: fields } });
          if (!placed.complete) setFormError(errorMessage(error, strings.connections.replaceFailed));
        },
      },
    );
  }

  return (
    <Form {...form}>
      <form onSubmit={form.handleSubmit(onSubmit)} noValidate className="grid gap-4">
        {formError && (
          <Alert variant="danger">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription>{formError}</AlertDescription>
          </Alert>
        )}
        <fieldset disabled={pending} className="grid gap-4">
          <SchemaFields
            control={form.control}
            prefix="credentials"
            fields={fields}
            mode="replace"
            hints={hints}
            disabled={pending}
          />
        </fieldset>
        {renderActions(
          <>
            {secondaryActions}
            <Button type="submit" disabled={pending || unsupported}>
              {pending && <Loader2Icon className="animate-spin" aria-hidden="true" />}
              {pending ? strings.connections.replacing : submitLabel}
            </Button>
          </>,
        )}
      </form>
    </Form>
  );
}
