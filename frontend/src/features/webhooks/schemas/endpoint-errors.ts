import type { Path, UseFormSetError } from "react-hook-form";
import { isApiError, type FieldError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import type { EndpointFormValues } from "./endpoint-form";

const e = strings.endpoints;
const RETRY_RANGE = { min: e.maxAttemptsRange, max: e.maxAttemptsRange, type: e.maxAttemptsRange };
const INTERVAL_RANGE = { min: e.intervalRange, max: e.intervalRange, type: e.intervalRange };

// Server rules (api/openapi.yaml, CreateWebhookEndpointRequest) by field, then rule.
const RULE_MESSAGES: Record<string, Record<string, string>> = {
  url: { required: e.urlRequired, url: e.urlInvalid, https: e.urlHttps },
  description: { max: e.descriptionTooLong, name: e.descriptionInvalid },
  events: { required: e.eventsRequired, max: e.eventsTooMany, oneof: e.eventUnknown, duplicate: e.eventDuplicate },
  api_version: { oneof: e.versionInvalid },
  headers: { max: e.headersTooMany },
  "headers.name": {
    required: e.headerNameInvalid,
    header_name: e.headerNameInvalid,
    reserved: e.headerNameReserved,
    duplicate: e.headerNameDuplicate,
  },
  "headers.value": { required: e.headerValueRequired, max: e.headerValueTooLong, header_value: e.headerValueInvalid },
  "retry_policy.max_attempts": RETRY_RANGE,
  "retry_policy.interval_seconds": INTERVAL_RANGE,
  "retry_policy.backoff": { oneof: e.backoffInvalid },
};

const EVENT_ITEM = /^events\.\d+$/;
const HEADER_ITEM = /^headers\.(\d+)\.(name|value)$/;

/** The form field a server error belongs to, or null when the form has no such field. */
function placeError(error: FieldError, headerRows: number): { field: string; message: string } | null {
  const unknown = strings.common.invalidField(error.rule);
  // Element errors land on the whole picker: its options are checkboxes, not one input per entry.
  if (EVENT_ITEM.test(error.field)) return { field: "events", message: RULE_MESSAGES.events?.[error.rule] ?? unknown };
  const header = HEADER_ITEM.exec(error.field);
  if (header) {
    if (Number(header[1]) >= headerRows) return null;
    return { field: error.field, message: RULE_MESSAGES[`headers.${header[2]}`]?.[error.rule] ?? unknown };
  }
  const messages = RULE_MESSAGES[error.field];
  if (!messages) return null;
  if (error.field === "headers" && headerRows === 0) return null;
  return { field: error.field, message: messages[error.rule] ?? unknown };
}

/**
 * Puts `invalid_request` field errors on the endpoint form. Returns true when
 * every error found a field, so the caller can skip the form-level alert.
 */
export function applyEndpointServerErrors(
  error: unknown,
  setError: UseFormSetError<EndpointFormValues>,
  headerRows: number,
): boolean {
  if (!isApiError(error) || error.code !== "invalid_request") return false;
  const placed = error.fields.map((f) => placeError(f, headerRows));
  const found = placed.filter((p): p is { field: string; message: string } => p !== null);
  found.forEach((p, index) =>
    setError(p.field as Path<EndpointFormValues>, { message: p.message }, { shouldFocus: index === 0 }),
  );
  return found.length > 0 && found.length === error.fields.length;
}
