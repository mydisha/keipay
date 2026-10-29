import { z } from "zod";
import { isHttpUrl } from "@/features/apps/schemas/app-form";
import { codePointLength, hasControlChars } from "@/shared/lib/credentials";
import { strings } from "@/shared/lib/strings";
import type {
  CreateWebhookEndpointRequest,
  UpdateWebhookEndpointRequest,
  WebhookEndpoint,
  WebhookRetryPolicy,
} from "../api/endpoints";

// Mirrors CreateWebhookEndpointRequest in api/openapi.yaml. The server checks
// again; these rules only give faster feedback.

export const MAX_DESCRIPTION_CHARS = 500;
export const MAX_EVENTS = 20;
export const MAX_HEADERS = 10;
export const MAX_HEADER_VALUE_CHARS = 1024;
export const RETRY_LIMITS = { maxAttempts: [1, 20], interval: [10, 3600] } as const;

/** RFC 9110 token characters, 1-64 of them. */
const HEADER_NAME = /^[!#$%&'*+\-.^_`|~0-9A-Za-z]{1,64}$/;
const RESERVED_HEADERS = new Set(["content-type", "content-length", "host", "user-agent", "connection", "transfer-encoding"]);
const RESERVED_PREFIXES = ["x-keipay-", "x-kei-"];

export function isReservedHeader(name: string): boolean {
  const lower = name.toLowerCase();
  return RESERVED_HEADERS.has(lower) || RESERVED_PREFIXES.some((prefix) => lower.startsWith(prefix));
}

/** Control characters other than newline and tab. */
function hasDescriptionControlChars(value: string): boolean {
  return [...value].some((char) => char !== "\n" && char !== "\t" && hasControlChars(char));
}

function wholeNumberIn(value: string, [min, max]: readonly [number, number]): boolean {
  if (!/^\d+$/.test(value.trim())) return false;
  const n = Number(value);
  return n >= min && n <= max;
}

const headerRow = z.object({ name: z.string(), value: z.string() });

export function endpointFormSchema(environment: "test" | "live") {
  return z
    .object({
      url: z
        .string()
        .trim()
        .min(1, strings.endpoints.urlRequired)
        // Blank already failed `min`; one message per field.
        .refine((url) => url === "" || isHttpUrl(url), strings.endpoints.urlInvalid)
        .refine((url) => environment !== "live" || !url.toLowerCase().startsWith("http:"), strings.endpoints.urlHttps),
      description: z
        .string()
        .trim()
        .refine((value) => codePointLength(value) <= MAX_DESCRIPTION_CHARS, strings.endpoints.descriptionTooLong)
        .refine((value) => !hasDescriptionControlChars(value), strings.endpoints.descriptionInvalid),
      events: z
        .array(z.string())
        .min(1, strings.endpoints.eventsRequired)
        .max(MAX_EVENTS, strings.endpoints.eventsTooMany),
      api_version: z.enum(["v1", "v2"], { error: strings.endpoints.versionInvalid }),
      legacy_signature: z.boolean(),
      /** Edit only: false keeps the stored headers and leaves `headers` out of the PATCH. */
      replace_headers: z.boolean(),
      headers: z.array(headerRow),
      retry_policy: z.object({
        max_attempts: z
          .string()
          .refine((value) => wholeNumberIn(value, RETRY_LIMITS.maxAttempts), strings.endpoints.maxAttemptsRange),
        backoff: z.enum(["exponential", "fixed"], { error: strings.endpoints.backoffInvalid }),
        interval_seconds: z
          .string()
          .refine((value) => wholeNumberIn(value, RETRY_LIMITS.interval), strings.endpoints.intervalRange),
      }),
    })
    .superRefine((values, ctx) => {
      if (!values.replace_headers) return;
      if (values.headers.length > MAX_HEADERS) {
        ctx.addIssue({ code: "custom", path: ["headers"], message: strings.endpoints.headersTooMany });
      }
      const seen = new Set<string>();
      values.headers.forEach((header, index) => {
        const name = header.name.trim();
        let nameError: string | null = null;
        if (!HEADER_NAME.test(name)) nameError = strings.endpoints.headerNameInvalid;
        else if (isReservedHeader(name)) nameError = strings.endpoints.headerNameReserved;
        else if (seen.has(name.toLowerCase())) nameError = strings.endpoints.headerNameDuplicate;
        seen.add(name.toLowerCase());
        if (nameError) ctx.addIssue({ code: "custom", path: ["headers", index, "name"], message: nameError });

        let valueError: string | null = null;
        if (header.value === "") valueError = strings.endpoints.headerValueRequired;
        else if (codePointLength(header.value) > MAX_HEADER_VALUE_CHARS) valueError = strings.endpoints.headerValueTooLong;
        else if (hasControlChars(header.value)) valueError = strings.endpoints.headerValueInvalid;
        if (valueError) ctx.addIssue({ code: "custom", path: ["headers", index, "value"], message: valueError });
      });
    });
}

export type EndpointFormValues = z.infer<ReturnType<typeof endpointFormSchema>>;

export function defaultEndpointValues(endpoint: WebhookEndpoint | null): EndpointFormValues {
  const retry = endpoint?.retry_policy ?? { max_attempts: 8, backoff: "exponential", interval_seconds: 30 };
  return {
    url: endpoint?.url ?? "",
    description: endpoint?.description ?? "",
    events: endpoint ? [...endpoint.events] : [],
    api_version: endpoint?.api_version ?? "v2",
    legacy_signature: endpoint?.legacy_signature ?? false,
    replace_headers: endpoint === null,
    headers: [],
    retry_policy: {
      max_attempts: String(retry.max_attempts),
      backoff: retry.backoff,
      interval_seconds: String(retry.interval_seconds),
    },
  };
}

function retryPolicy(values: EndpointFormValues): WebhookRetryPolicy {
  return {
    max_attempts: Number(values.retry_policy.max_attempts),
    backoff: values.retry_policy.backoff,
    interval_seconds: Number(values.retry_policy.interval_seconds),
  };
}

function headerInputs(values: EndpointFormValues) {
  return values.headers.map((header) => ({ name: header.name.trim(), value: header.value }));
}

export function toCreateRequest(values: EndpointFormValues): CreateWebhookEndpointRequest {
  const body: CreateWebhookEndpointRequest = {
    url: values.url,
    events: values.events,
    api_version: values.api_version,
    legacy_signature: values.legacy_signature,
    retry_policy: retryPolicy(values),
  };
  if (values.description) body.description = values.description;
  if (values.headers.length > 0) body.headers = headerInputs(values);
  return body;
}

function sameList(a: readonly string[], b: readonly string[]): boolean {
  return a.length === b.length && a.every((item, index) => item === b[index]);
}

/** Only what differs from the endpoint: PATCH semantics, and the audit log records just those. */
export function toUpdateRequest(endpoint: WebhookEndpoint, values: EndpointFormValues): UpdateWebhookEndpointRequest {
  const body: UpdateWebhookEndpointRequest = {};
  if (values.url !== endpoint.url) body.url = values.url;
  if (values.description !== endpoint.description) body.description = values.description;
  if (!sameList(values.events, endpoint.events)) body.events = values.events;
  if (values.api_version !== endpoint.api_version) body.api_version = values.api_version;
  if (values.legacy_signature !== endpoint.legacy_signature) body.legacy_signature = values.legacy_signature;
  if (values.replace_headers) body.headers = headerInputs(values);
  const retry = retryPolicy(values);
  const current = endpoint.retry_policy;
  if (
    retry.max_attempts !== current.max_attempts ||
    retry.backoff !== current.backoff ||
    retry.interval_seconds !== current.interval_seconds
  ) {
    body.retry_policy = retry;
  }
  return body;
}
