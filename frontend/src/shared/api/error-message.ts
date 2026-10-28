import { strings } from "@/shared/lib/strings";
import { isApiError, type ApiError } from "./errors";

function sentenceCase(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return strings.errors.generic;
  const sentence = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
}

/** Whole minutes until a `rate_limited` error can be retried (at least 1). */
export function retryAfterMinutes(error: ApiError): number {
  const seconds = error.details.retry_after_seconds;
  if (typeof seconds !== "number" || !Number.isFinite(seconds) || seconds <= 0) return 1;
  return Math.max(1, Math.ceil(seconds / 60));
}

/** A message that is safe and useful to show for any thrown value. */
export function errorMessage(error: unknown, fallback: string = strings.errors.generic): string {
  if (!isApiError(error)) return fallback;
  switch (error.code) {
    case "network_error":
      return strings.errors.network;
    case "unknown_error":
      return strings.errors.unknown;
    case "internal_error":
      return fallback;
    case "forbidden":
      return strings.errors.forbidden;
    case "invalid_csrf_token":
      return strings.errors.csrf;
    case "session_expired":
    case "session_required":
      return strings.errors.sessionExpired;
    case "rate_limited":
      return strings.errors.rateLimited(retryAfterMinutes(error));
    default:
      return sentenceCase(error.message);
  }
}
