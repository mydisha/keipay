import type { FieldValues, Path, UseFormSetError } from "react-hook-form";
import { isApiError } from "@/shared/api/errors";
import { strings } from "./strings";

/** Messages for server field rules, by JSON field name then rule. */
export type FieldRuleMessages = Record<string, Record<string, string>>;

const PASSWORD_RULES = { min: strings.auth.passwordTooShort, max: strings.auth.passwordTooLong };

/** Server rules for the email, name and password fields (see api/openapi.yaml). */
export const credentialRuleMessages: FieldRuleMessages = {
  email: { required: strings.auth.emailRequired, email: strings.auth.emailInvalid, max: strings.auth.emailInvalid },
  name: { required: strings.auth.nameRequired, max: strings.auth.nameTooLong, name: strings.auth.nameInvalid },
  password: PASSWORD_RULES,
  new_password: PASSWORD_RULES,
};

/**
 * Puts `invalid_request` field errors on their inputs. Returns true when every
 * error was placed on a field, so the caller can skip the form-level alert.
 */
export function applyFieldErrors<T extends FieldValues>(
  error: unknown,
  setError: UseFormSetError<T>,
  fieldNames: readonly string[],
  messages: FieldRuleMessages = credentialRuleMessages,
): boolean {
  if (!isApiError(error) || error.code !== "invalid_request") return false;
  const placed = error.fields.filter((f) => fieldNames.includes(f.field));
  placed.forEach((f, index) => {
    const message = messages[f.field]?.[f.rule] ?? strings.common.invalidField(f.rule);
    setError(f.field as Path<T>, { message }, { shouldFocus: index === 0 });
  });
  return placed.length > 0 && placed.length === error.fields.length;
}
