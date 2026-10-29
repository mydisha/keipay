import { isApiError } from "@/shared/api/errors";

/** A malformed id is a 400 (rule `public_id`); to the user it is just not there. */
export function isNotFound(error: unknown): boolean {
  return isApiError(error) && (error.code === "not_found" || error.code === "invalid_request");
}
