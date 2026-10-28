import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { isApiError, type FieldError } from "@/shared/api/errors";
import { NAME_RULES } from "@/shared/lib/field-errors";
import { strings } from "@/shared/lib/strings";

const URL_RULES = { url: strings.apps.urlInvalid };

// Server rules (api/openapi.yaml) by JSON field, then rule.
const RULE_MESSAGES: Record<string, Record<string, string>> = {
  name: NAME_RULES,
  slug: { required: strings.apps.slugInvalid, slug: strings.apps.slugInvalid },
  webhook_url: URL_RULES,
  redirect_success_url: URL_RULES,
  redirect_failure_url: URL_RULES,
  metadata: {
    object: strings.apps.metadataObject,
    max_keys: strings.apps.metadataMaxKeys,
    key: strings.apps.metadataKeyInvalid,
    max_bytes: strings.apps.metadataTooLarge,
  },
  label: { required: strings.keys.labelRequired, max: strings.keys.labelTooLong, name: strings.keys.labelInvalid },
  expires_at: { future: strings.keys.expiresFuture },
};

const METADATA_VALUE_RULES: Record<string, (key: string) => string> = {
  type: strings.apps.metadataValueType,
  max: strings.apps.metadataValueTooLong,
  invalid_character: strings.apps.metadataValueNull,
};

const METADATA_VALUE_PREFIX = "metadata.";

/** The form field a server field error belongs to, and its message. */
function placeError(error: FieldError): { field: string; message: string } {
  if (error.field.startsWith(METADATA_VALUE_PREFIX)) {
    const key = error.field.slice(METADATA_VALUE_PREFIX.length);
    const message = METADATA_VALUE_RULES[error.rule]?.(key) ?? strings.common.invalidField(error.rule);
    return { field: "metadata", message };
  }
  return { field: error.field, message: RULE_MESSAGES[error.field]?.[error.rule] ?? strings.common.invalidField(error.rule) };
}

interface ServerErrorOptions {
  /** Shown on the slug field for `409 conflict`; without it a conflict goes to the alert. */
  slugConflict?: string;
}

/**
 * Puts server-side field errors on their inputs. Returns true when every error
 * was placed on a field, so the caller can skip the form-level alert.
 */
export function applyServerErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fieldNames: readonly string[],
  { slugConflict }: ServerErrorOptions = {},
): boolean {
  if (!isApiError(error)) return false;
  if (error.code === "conflict") {
    if (!slugConflict || !fieldNames.includes("slug")) return false;
    setError("slug" as Path<T>, { message: slugConflict }, { shouldFocus: true });
    return true;
  }
  if (error.code !== "invalid_request") return false;
  const placed = error.fields.map(placeError).filter((p) => fieldNames.includes(p.field));
  placed.forEach((p, index) => setError(p.field as Path<T>, { message: p.message }, { shouldFocus: index === 0 }));
  return placed.length > 0 && placed.length === error.fields.length;
}
