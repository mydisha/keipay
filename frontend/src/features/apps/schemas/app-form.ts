import { z } from "zod";
import { hasControlChars, nameField, utf8Length } from "@/shared/lib/credentials";
import { strings } from "@/shared/lib/strings";
import { metadataText } from "./metadata";

// Mirrors CreateAppRequest / UpdateAppRequest in api/openapi.yaml.
export const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$/;
const URL_MAX_BYTES = 2048;

function isHttpUrl(value: string): boolean {
  if (/\s/.test(value) || hasControlChars(value)) return false;
  if (utf8Length(value) > URL_MAX_BYTES) return false;
  try {
    const url = new URL(value);
    return (url.protocol === "http:" || url.protocol === "https:") && url.hostname !== "";
  } catch {
    return false;
  }
}

const optionalUrl = z
  .string()
  .trim()
  .refine((value) => value === "" || isHttpUrl(value), strings.apps.urlInvalid);

const name = nameField();

/** What the slug field suggests for a name: "Acme Store!" → "acme-store". */
export function suggestSlug(fromName: string): string {
  return fromName
    .normalize("NFKD")
    .replace(/[̀-ͯ]/g, "")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 32)
    .replace(/-+$/, "");
}

export const createAppSchema = z.object({
  name,
  slug: z.string().trim().toLowerCase().regex(SLUG_PATTERN, strings.apps.slugInvalid),
  environment: z.enum(["test", "live"]),
  webhook_url: optionalUrl,
  redirect_success_url: optionalUrl,
  redirect_failure_url: optionalUrl,
  metadata: metadataText,
});

export type CreateAppValues = z.infer<typeof createAppSchema>;

export const editAppSchema = z.object({
  name,
  webhook_url: optionalUrl,
  redirect_success_url: optionalUrl,
  redirect_failure_url: optionalUrl,
  metadata: metadataText,
});

export type EditAppValues = z.infer<typeof editAppSchema>;
