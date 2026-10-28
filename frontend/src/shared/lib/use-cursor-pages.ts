import { useEffect, useState } from "react";
import { toast } from "sonner";
import { isApiError } from "@/shared/api/errors";

/**
 * A stale or tampered cursor (`invalid_cursor`) goes back to the newest page
 * with a notice instead of an error. Returns `resetting` (show the table as
 * loading meanwhile) and a `resetKey` for DataTable that changes on each reset.
 */
export function useInvalidCursorReset(
  error: unknown,
  cursor: string | null,
  setCursor: (cursor: string | null) => void,
  message: string,
): { resetting: boolean; resetKey: string } {
  const [resetCount, setResetCount] = useState(0);
  const resetting = isApiError(error) && error.code === "invalid_cursor" && cursor !== null;

  useEffect(() => {
    if (!resetting) return;
    toast.info(message);
    setCursor(null);
    setResetCount((count) => count + 1);
  }, [resetting, message, setCursor]);

  return { resetting, resetKey: String(resetCount) };
}
