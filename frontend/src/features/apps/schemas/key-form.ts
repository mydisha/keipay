import { z } from "zod";
import { textField } from "@/shared/lib/credentials";
import { strings } from "@/shared/lib/strings";

// Mirrors CreateApiKeyRequest in api/openapi.yaml.
const LABEL_MAX = 60;
const LOCAL_DATE_TIME = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2})?$/;

/**
 * Reads a `datetime-local` value as Asia/Jakarta time (the console shows every
 * time in WIB, which has no daylight saving), or null when it isn't one.
 */
export function wibInputToDate(value: string): Date | null {
  if (!LOCAL_DATE_TIME.test(value)) return null;
  const withSeconds = value.length === 16 ? `${value}:00` : value;
  const date = new Date(`${withSeconds}+07:00`);
  return Number.isNaN(date.getTime()) ? null : date;
}

export const createKeySchema = z.object({
  label: textField({
    max: LABEL_MAX,
    messages: { required: strings.keys.labelRequired, tooLong: strings.keys.labelTooLong, invalid: strings.keys.labelInvalid },
  }),
  expires_at: z.string().refine((value) => {
    if (value === "") return true;
    const date = wibInputToDate(value);
    return date !== null && date.getTime() > Date.now();
  }, strings.keys.expiresFuture),
});

export type CreateKeyValues = z.infer<typeof createKeySchema>;
