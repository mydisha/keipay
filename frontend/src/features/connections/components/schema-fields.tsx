import type { ReactNode } from "react";
import type { Control, FieldPath, FieldValues } from "react-hook-form";
import { strings } from "@/shared/lib/strings";
import { FormControl, FormDescription, FormField, FormItem, FormLabel, FormMessage } from "@/shared/ui/form";
import { Input } from "@/shared/ui/input";
import { PasswordInput } from "@/shared/ui/password-input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/shared/ui/select";
import { Switch } from "@/shared/ui/switch";
import type { SchemaField } from "../api/connections";
import { isSupportedField, type SchemaFormMode } from "../schemas/schema-form";

// Radix Select can't use "" as an item value; this stands for "no value".
const DEFAULT_OPTION = "__default__";

interface SchemaFieldsProps<T extends FieldValues> {
  control: Control<T>;
  /** Where the values live in the form: `credentials` or `settings`. */
  prefix: "credentials" | "settings";
  fields: readonly SchemaField[];
  mode?: SchemaFormMode;
  /** Replace mode: the stored value's last 4 characters per key (null when too short to show). */
  hints?: Record<string, string | null>;
  disabled?: boolean;
}

function FieldLabel({ field, mode }: { field: SchemaField; mode: SchemaFormMode }) {
  const marked = mode === "create" && field.required;
  return (
    <FormLabel>
      {field.label}
      {marked && (
        <span className="text-danger-foreground" aria-hidden="true">
          *
        </span>
      )}
    </FormLabel>
  );
}

function replacePlaceholder(hint: string | null | undefined): string {
  return hint ? strings.connections.maskedHint(hint) : strings.connections.stored;
}

/** Renders a provider's credential or settings fields from its schema, in schema order. */
export function SchemaFields<T extends FieldValues>({
  control,
  prefix,
  fields,
  mode = "create",
  hints = {},
  disabled = false,
}: SchemaFieldsProps<T>) {
  return (
    <>
      {fields.map((field) => {
        const name = `${prefix}.${field.key}` as FieldPath<T>;
        if (!isSupportedField(field)) return <UnsupportedField key={field.key} field={field} />;
        const required = mode === "create" && field.required;
        return (
          <FormField
            key={field.key}
            control={control}
            name={name}
            render={({ field: input }) => {
              let widget: ReactNode;
              if (field.type === "boolean") {
                return (
                  <FormItem>
                    <div className="flex items-center justify-between gap-4 max-md:min-h-11">
                      <FieldLabel field={field} mode={mode} />
                      <FormControl>
                        <Switch
                          checked={input.value === true}
                          onCheckedChange={input.onChange}
                          onBlur={input.onBlur}
                          ref={input.ref}
                          disabled={disabled}
                        />
                      </FormControl>
                    </div>
                    {field.description && <FormDescription>{field.description}</FormDescription>}
                    <FormMessage />
                  </FormItem>
                );
              }
              const text = typeof input.value === "string" ? input.value : "";
              if (field.type === "enum") {
                widget = (
                  <Select
                    value={text === "" ? (required ? undefined : DEFAULT_OPTION) : text}
                    onValueChange={(value) => input.onChange(value === DEFAULT_OPTION ? "" : value)}
                    disabled={disabled}
                  >
                    <FormControl>
                      <SelectTrigger ref={input.ref} onBlur={input.onBlur} aria-required={required || undefined}>
                        <SelectValue placeholder={field.placeholder || field.label} />
                      </SelectTrigger>
                    </FormControl>
                    <SelectContent>
                      {!required && <SelectItem value={DEFAULT_OPTION}>{strings.connections.defaultOption}</SelectItem>}
                      {field.options?.map((option) => (
                        <SelectItem key={option.value} value={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                );
              } else if (field.secret) {
                widget = (
                  <FormControl>
                    <PasswordInput
                      {...input}
                      value={text}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      className="pr-11 font-mono"
                      placeholder={mode === "replace" ? replacePlaceholder(hints[field.key]) : field.placeholder}
                      showLabel={`${strings.connections.secretShow}: ${field.label}`}
                      hideLabel={`${strings.connections.secretHide}: ${field.label}`}
                      aria-required={required || undefined}
                      disabled={disabled}
                    />
                  </FormControl>
                );
              } else {
                widget = (
                  <FormControl>
                    <Input
                      {...input}
                      value={text}
                      autoComplete="off"
                      autoCapitalize="off"
                      spellCheck={false}
                      inputMode={field.type === "integer" ? "numeric" : undefined}
                      className={field.type === "integer" ? "tabular-nums" : undefined}
                      placeholder={mode === "replace" ? replacePlaceholder(hints[field.key]) : field.placeholder}
                      aria-required={required || undefined}
                      disabled={disabled}
                    />
                  </FormControl>
                );
              }
              return (
                <FormItem>
                  <FieldLabel field={field} mode={mode} />
                  {widget}
                  {field.description && <FormDescription>{field.description}</FormDescription>}
                  <FormMessage />
                </FormItem>
              );
            }}
          />
        );
      })}
    </>
  );
}

function UnsupportedField({ field }: { field: SchemaField }) {
  return (
    <div className="grid gap-1 text-sm" data-testid="unsupported-field">
      <span className="font-medium">{field.label}</span>
      <p className="text-xs font-medium text-danger-foreground">{strings.connections.unsupportedField}</p>
    </div>
  );
}
