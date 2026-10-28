import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";

/**
 * Puts server-side field errors on their inputs. Returns true when every error
 * was placed on a field, so the caller can skip the form-level alert.
 */
export function applyServerErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fieldNames: readonly string[],
): boolean {
  if (!isApiError(error)) return false;
  if (error.code === "conflict" && fieldNames.includes("slug")) {
    setError("slug" as Path<T>, { message: strings.apps.slugTaken }, { shouldFocus: true });
    return true;
  }
  const fields = error.fields.filter((f) => fieldNames.includes(f.field));
  if (error.code !== "invalid_request" || fields.length === 0) return false;
  for (const f of fields) setError(f.field as Path<T>, { message: strings.common.invalidField(f.rule) });
  return fields.length === error.fields.length;
}
