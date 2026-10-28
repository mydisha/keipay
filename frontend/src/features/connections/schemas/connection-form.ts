import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { z } from "zod";
import { isApiError, type FieldError } from "@/shared/api/errors";
import { formatNumber } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import type { SchemaField } from "../api/connections";

const NAME_PATTERN = /^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/;

/** The server trims and lowers a connection name before checking it. */
export function normalizeName(name: string): string {
  return name.trim().toLowerCase();
}

export const connectionNameSchema = z.string().superRefine((raw, ctx) => {
  const name = normalizeName(raw);
  if (name === "") ctx.addIssue({ code: "custom", message: strings.connections.nameRequired });
  else if (!NAME_PATTERN.test(name)) ctx.addIssue({ code: "custom", message: strings.connections.nameInvalid });
});

/** `xendit-test`: the name the wizard suggests until the user edits it. */
export function suggestName(provider: string, environment: string): string {
  return normalizeName(`${provider}-${environment}`).replace(/[^a-z0-9-]+/g, "-");
}

/** Schemas of the form sections whose server field paths start with `credentials.` or `settings.`. */
export interface SchemaSections {
  credentials?: readonly SchemaField[];
  settings?: readonly SchemaField[];
}

const NAME_RULES: Record<string, string> = {
  required: strings.connections.nameRequired,
  slug: strings.connections.nameInvalid,
};

function schemaRuleMessage(field: SchemaField | undefined, rule: string): string {
  const messages = strings.connections.field;
  switch (rule) {
    case "required":
      return messages.required;
    case "min_length":
      return field?.min_length !== undefined ? messages.minLength(field.min_length) : messages.type;
    case "max_length":
      return field?.max_length !== undefined ? messages.maxLength(field.max_length) : messages.type;
    case "pattern":
      return field?.pattern_hint || messages.pattern;
    case "minimum":
      return field?.minimum !== undefined ? messages.minimum(formatNumber(field.minimum)) : messages.integer;
    case "maximum":
      return field?.maximum !== undefined ? messages.maximum(formatNumber(field.maximum)) : messages.integer;
    case "oneof":
      return messages.oneof;
    case "type":
      return field?.type === "integer" ? messages.integer : messages.type;
    case "unknown":
      return messages.unknown;
    case "environment":
      return messages.environment;
    case "immutable":
      return messages.immutable;
    default:
      return strings.common.invalidField(rule);
  }
}

/** The form field and message for one server field error, or null when the form has no such field. */
function placeError(error: FieldError, sections: SchemaSections): { field: string; message: string } | null {
  const [section, key] = error.field.split(".", 2);
  if ((section === "credentials" || section === "settings") && key) {
    const fields = sections[section];
    if (!fields) return null;
    const field = fields.find((f) => f.key === key);
    return field ? { field: error.field, message: schemaRuleMessage(field, error.rule) } : null;
  }
  if (error.field === "name") return { field: "name", message: NAME_RULES[error.rule] ?? strings.common.invalidField(error.rule) };
  if (error.field === "environment") return { field: "environment", message: strings.connections.environmentInvalid };
  if (error.field === "provider") return { field: "provider", message: strings.connections.providerRequired };
  return null;
}

export interface PlacedErrors {
  /** Form fields that received a message, in server order. */
  fields: string[];
  /** True when every server error found a field, so no form-level alert is needed. */
  complete: boolean;
}

/**
 * Puts `400 invalid_request` field errors and a `409 conflict` (the name) on
 * their inputs. Only fields listed in `fieldNames` or in `sections` are used.
 */
export function applyConnectionErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  { fieldNames = [], sections = {} }: { fieldNames?: readonly string[]; sections?: SchemaSections },
): PlacedErrors {
  if (!isApiError(error)) return { fields: [], complete: false };
  if (error.code === "conflict") {
    if (!fieldNames.includes("name")) return { fields: [], complete: false };
    setError("name" as Path<T>, { message: strings.connections.nameTaken }, { shouldFocus: true });
    return { fields: ["name"], complete: true };
  }
  if (error.code !== "invalid_request") return { fields: [], complete: false };
  const placed = error.fields
    .map((f) => placeError(f, sections))
    .filter((p): p is { field: string; message: string } => p !== null)
    .filter((p) => p.field.includes(".") || fieldNames.includes(p.field));
  placed.forEach((p, index) => setError(p.field as Path<T>, { message: p.message }, { shouldFocus: index === 0 }));
  return { fields: placed.map((p) => p.field), complete: placed.length > 0 && placed.length === error.fields.length };
}
