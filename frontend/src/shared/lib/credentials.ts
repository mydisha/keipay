import { z } from "zod";
import { strings } from "./strings";

// Mirrors the Email, UserName and NewPassword rules in api/openapi.yaml (the
// server validates again; these only give faster, friendlier feedback).

const encoder = new TextEncoder();

export function utf8Length(value: string): number {
  return encoder.encode(value).length;
}

export function codePointLength(value: string): number {
  return [...value].length;
}

/** Trimmed, NFC and lower case: the form the server stores and matches. */
export function canonicalEmail(value: string): string {
  return value.trim().normalize("NFC").toLowerCase();
}

// Whitespace, control characters and the characters that only appear in
// quoted or display-name forms.
const FORBIDDEN_EMAIL_CHARS = /[\s\p{Cc}<>()[\]\\,;:"]/u;
const CONTROL_CHARS = /\p{Cc}/u;

export function isValidEmail(value: string): boolean {
  const email = canonicalEmail(value);
  const bytes = utf8Length(email);
  if (bytes < 3 || bytes > 254) return false;
  if (FORBIDDEN_EMAIL_CHARS.test(email)) return false;
  const parts = email.split("@");
  if (parts.length !== 2) return false;
  const [local = "", domain = ""] = parts;
  const localBytes = utf8Length(local);
  const domainBytes = utf8Length(domain);
  if (localBytes < 1 || localBytes > 64) return false;
  if (domainBytes < 1 || domainBytes > 253) return false;
  if (!domain.includes(".") || domain.startsWith(".") || domain.endsWith(".") || domain.includes("..")) return false;
  return true;
}

export const MIN_PASSWORD_CHARS = 12;
export const MAX_PASSWORD_BYTES = 72;
export const MAX_NAME_CHARS = 100;

/** An email for a new or edited user: trimmed before it is sent. */
export function emailField() {
  return z.string().trim().min(1, strings.auth.emailRequired).refine(isValidEmail, strings.auth.emailInvalid);
}

export function nameField() {
  return z
    .string()
    .trim()
    .min(1, strings.auth.nameRequired)
    .refine((value) => codePointLength(value) <= MAX_NAME_CHARS, strings.auth.nameTooLong)
    .refine((value) => !CONTROL_CHARS.test(value), strings.auth.nameInvalid);
}

/** A new password, sent exactly as typed (no trimming or normalization). */
export function newPasswordField() {
  return z
    .string()
    .refine((value) => codePointLength(value) >= MIN_PASSWORD_CHARS, strings.auth.passwordTooShort)
    .refine((value) => utf8Length(value) <= MAX_PASSWORD_BYTES, strings.auth.passwordTooLong);
}
