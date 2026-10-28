import { z } from "zod";
import { strings } from "@/shared/lib/strings";

// Mirrors CreateTenantRequest / UpdateTenantRequest in api/openapi.yaml.
export const SLUG_PATTERN = /^[a-z0-9][a-z0-9-]{1,30}[a-z0-9]$/;

function isHttpUrl(value: string): boolean {
  try {
    const url = new URL(value);
    return url.protocol === "http:" || url.protocol === "https:";
  } catch {
    return false;
  }
}

const optionalUrl = z
  .string()
  .trim()
  .refine((value) => value === "" || isHttpUrl(value), strings.apps.urlInvalid);

const name = z.string().trim().min(1, strings.apps.nameRequired);

export const createAppSchema = z.object({
  name,
  slug: z.string().trim().regex(SLUG_PATTERN, strings.apps.slugInvalid),
  webhook_url: optionalUrl,
  redirect_success_url: optionalUrl,
  redirect_failure_url: optionalUrl,
});

export type CreateAppValues = z.infer<typeof createAppSchema>;

export const editAppSchema = z.object({
  name,
  webhook_url: optionalUrl,
  redirect_success_url: optionalUrl,
  redirect_failure_url: optionalUrl,
  active: z.boolean(),
});

export type EditAppValues = z.infer<typeof editAppSchema>;
