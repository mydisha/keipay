import { z } from "zod";
import { formatNumber } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import type { SchemaField } from "../api/connections";

// Builds forms from the `SchemaField` lists a provider serves. The rules mirror
// the server's generic validator (api/openapi.yaml `SchemaField`), so most
// mistakes show before a request; the server stays the authority.

/** Form state for one schema: text for string, integer and enum fields, a boolean for switches. */
export type SchemaValues = Record<string, string | boolean>;

/** `create` applies every rule; `replace` (credentials) makes every field optional, blank meaning "keep". */
export type SchemaFormMode = "create" | "replace";

const SUPPORTED_TYPES: readonly string[] = ["string", "integer", "boolean", "enum"];
const INTEGER = /^[+-]?\d+$/;

export function isSupportedField(field: SchemaField): boolean {
  return SUPPORTED_TYPES.includes(field.type);
}

/** A field type this console can't render makes the whole form unsafe to submit. */
export function hasUnsupportedField(fields: readonly SchemaField[]): boolean {
  return fields.some((field) => !isSupportedField(field));
}

/** Length in code points, as the server counts it. */
export function codePointLength(value: string): number {
  return [...value].length;
}

/** The pattern anchored to the whole value; null when JavaScript can't compile it (the server still checks). */
function compilePattern(pattern: string): RegExp | null {
  for (const flags of ["u", ""]) {
    try {
      return new RegExp(`^(?:${pattern})$`, flags);
    } catch {
      // Try without the unicode flag, then give up.
    }
  }
  return null;
}

function checkString(field: SchemaField, value: string): string | null {
  const length = codePointLength(value);
  if (field.min_length !== undefined && length < field.min_length) return strings.connections.field.minLength(field.min_length);
  if (field.max_length !== undefined && length > field.max_length) return strings.connections.field.maxLength(field.max_length);
  if (field.pattern) {
    const pattern = compilePattern(field.pattern);
    if (pattern && !pattern.test(value)) return field.pattern_hint || strings.connections.field.pattern;
  }
  return null;
}

function checkInteger(field: SchemaField, value: string): string | null {
  if (!INTEGER.test(value)) return strings.connections.field.integer;
  const number = BigInt(value);
  if (field.minimum !== undefined && number < BigInt(field.minimum)) {
    return strings.connections.field.minimum(formatNumber(field.minimum));
  }
  if (field.maximum !== undefined && number > BigInt(field.maximum)) {
    return strings.connections.field.maximum(formatNumber(field.maximum));
  }
  if (!Number.isSafeInteger(Number(value))) return strings.connections.field.integer;
  return null;
}

function checkEnum(field: SchemaField, value: string): string | null {
  return field.options?.some((option) => option.value === value) ? null : strings.connections.field.oneof;
}

/** The message for a typed value, or null when it passes. Blank values are handled by the caller. */
function checkValue(field: SchemaField, value: string): string | null {
  switch (field.type) {
    case "string":
      return checkString(field, value);
    case "integer":
      return checkInteger(field, value);
    case "enum":
      return checkEnum(field, value);
    default:
      return null;
  }
}

function fieldSchema(field: SchemaField, mode: SchemaFormMode): z.ZodType<string | boolean | undefined> {
  if (!isSupportedField(field)) return z.unknown().optional() as z.ZodType<undefined>;
  if (field.type === "boolean") return z.boolean();
  return z.string().superRefine((raw, ctx) => {
    const value = raw.trim();
    if (value === "") {
      if (mode === "create" && field.required) ctx.addIssue({ code: "custom", message: strings.connections.field.required });
      return;
    }
    const message = checkValue(field, value);
    if (message) ctx.addIssue({ code: "custom", message });
  });
}

/** A zod schema for one `ConfigSchema`'s form values. */
export function schemaFormSchema(fields: readonly SchemaField[], mode: SchemaFormMode = "create"): z.ZodType<SchemaValues, SchemaValues> {
  const shape: Record<string, z.ZodType<string | boolean | undefined>> = {};
  for (const field of fields) shape[field.key] = fieldSchema(field, mode);
  const object = z.object(shape) as unknown as z.ZodType<SchemaValues, SchemaValues>;
  if (mode === "create") return object;
  return object.superRefine((values, ctx) => {
    const textFields = fields.filter((field) => isSupportedField(field) && field.type !== "boolean");
    const anyValue = textFields.some((field) => String(values[field.key] ?? "").trim() !== "");
    const first = textFields[0];
    if (!anyValue && first) {
      ctx.addIssue({ code: "custom", message: strings.connections.replaceAtLeastOne, path: [first.key] });
    }
  });
}

function defaultFor(field: SchemaField, current: unknown): string | boolean {
  if (field.type === "boolean") {
    if (typeof current === "boolean") return current;
    return typeof field.default === "boolean" ? field.default : false;
  }
  // Secrets are never prefilled, not even from the server.
  if (field.secret) return "";
  const value = current ?? field.default;
  return value === null || value === undefined ? "" : String(value);
}

/** Initial form values: the current value, else the schema default, else empty. */
export function schemaDefaults(fields: readonly SchemaField[], current: Record<string, unknown> = {}): SchemaValues {
  const values: SchemaValues = {};
  for (const field of fields) {
    if (isSupportedField(field)) values[field.key] = defaultFor(field, current[field.key]);
  }
  return values;
}

/** Request values from form values: trimmed, blanks left out, integers as numbers. */
export function schemaPayload(fields: readonly SchemaField[], values: SchemaValues): Record<string, string | number | boolean> {
  const payload: Record<string, string | number | boolean> = {};
  for (const field of fields) {
    if (!isSupportedField(field)) continue;
    const raw = values[field.key];
    if (typeof raw === "boolean") {
      payload[field.key] = raw;
      continue;
    }
    const value = (raw ?? "").trim();
    if (value === "") continue;
    payload[field.key] = field.type === "integer" ? Number(value) : value;
  }
  return payload;
}

/** Credential values to send: only the filled-in ones, as strings. */
export function credentialPayload(fields: readonly SchemaField[], values: SchemaValues): Record<string, string> {
  const payload: Record<string, string> = {};
  for (const [key, value] of Object.entries(schemaPayload(fields, values))) payload[key] = String(value);
  return payload;
}
