import { PlusIcon, Trash2Icon } from "lucide-react";
import { useFieldArray, useFormContext } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { FormControl, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { PasswordInput } from "@/shared/ui/password-input";
import type { WebhookEndpoint } from "../api/endpoints";
import { MAX_HEADERS, type EndpointFormValues } from "../schemas/endpoint-form";

interface HeadersEditorProps {
  /** The endpoint being edited; null when adding one. */
  endpoint: WebhookEndpoint | null;
  disabled?: boolean;
}

/**
 * Custom header rows (name + value). Stored values are never returned, so an
 * edit shows the names and hints and offers "Replace headers", which asks for
 * the whole set again.
 */
export function HeadersEditor({ endpoint, disabled = false }: HeadersEditorProps) {
  const form = useFormContext<EndpointFormValues>();
  const { fields, append, remove, replace } = useFieldArray({ control: form.control, name: "headers" });
  const replacing = form.watch("replace_headers");
  const rootError = form.formState.errors.headers?.root?.message ?? form.formState.errors.headers?.message;

  function startReplacing() {
    replace((endpoint?.headers ?? []).map((hint) => ({ name: hint.name, value: "" })));
    form.setValue("replace_headers", true);
  }

  function keepCurrent() {
    replace([]);
    form.clearErrors("headers");
    form.setValue("replace_headers", false);
  }

  return (
    <fieldset className="grid gap-3" disabled={disabled}>
      <legend className="mb-1 text-sm font-medium">{strings.endpoints.headers}</legend>
      <p className="-mt-2 text-xs text-muted-foreground">
        {replacing && endpoint ? strings.endpoints.replaceHeadersHint : strings.endpoints.headersHint}
      </p>

      {!replacing && endpoint && (
        <>
          {endpoint.headers.length === 0 ? (
            <p className="text-sm text-muted-foreground">{strings.endpoints.noHeaders}</p>
          ) : (
            <ul className="divide-y border-y text-sm">
              {endpoint.headers.map((hint) => (
                <li key={hint.name} className="flex min-w-0 items-center justify-between gap-3 py-2">
                  <span className="truncate font-mono text-[0.8125rem]" title={hint.name}>
                    {hint.name}
                  </span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">
                    {hint.value_last4 ? strings.endpoints.secretHint(hint.value_last4) : strings.endpoints.storedHeaderValue}
                  </span>
                </li>
              ))}
            </ul>
          )}
          <div>
            <Button variant="outline" size="sm" onClick={startReplacing}>
              {strings.endpoints.replaceHeaders}
            </Button>
          </div>
        </>
      )}

      {replacing && (
        <>
          {fields.length === 0 && <p className="text-sm text-muted-foreground">{strings.endpoints.noHeaders}</p>}
          <ul className="divide-y border-y">
            {fields.map((row, index) => (
              <li key={row.id} className="grid gap-2 py-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto] sm:items-start">
                <FormField
                  control={form.control}
                  name={`headers.${index}.name`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel className="text-xs">{strings.endpoints.headerNameN(index + 1)}</FormLabel>
                      <FormControl>
                        <Input
                          autoComplete="off"
                          autoCapitalize="off"
                          spellCheck={false}
                          placeholder="Authorization"
                          className="font-mono"
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name={`headers.${index}.value`}
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel className="text-xs">{strings.endpoints.headerValueN(index + 1)}</FormLabel>
                      <FormControl>
                        <PasswordInput
                          autoComplete="off"
                          spellCheck={false}
                          showLabel={strings.connections.secretShow}
                          hideLabel={strings.connections.secretHide}
                          {...field}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <Button
                  variant="ghost"
                  size="icon-sm"
                  className="justify-self-end sm:mt-6"
                  onClick={() => remove(index)}
                  aria-label={strings.endpoints.removeHeader(index + 1)}
                >
                  <Trash2Icon aria-hidden="true" />
                </Button>
              </li>
            ))}
          </ul>
          <div className="flex flex-wrap gap-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => append({ name: "", value: "" })}
              disabled={fields.length >= MAX_HEADERS}
            >
              <PlusIcon aria-hidden="true" />
              {strings.endpoints.addHeader}
            </Button>
            {endpoint && (
              <Button variant="ghost" size="sm" onClick={keepCurrent}>
                {strings.endpoints.keepHeaders}
              </Button>
            )}
          </div>
        </>
      )}

      {rootError && <p className="text-xs font-medium text-danger-foreground">{rootError}</p>}
    </fieldset>
  );
}
