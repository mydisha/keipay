import { strings } from "@/shared/lib/strings";
import { isApiError } from "./errors";

function sentenceCase(text: string): string {
  const trimmed = text.trim();
  if (!trimmed) return strings.errors.generic;
  const sentence = trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
  return /[.!?]$/.test(sentence) ? sentence : `${sentence}.`;
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
    default:
      return sentenceCase(error.message);
  }
}
