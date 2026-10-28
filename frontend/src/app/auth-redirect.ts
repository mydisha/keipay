import type { QueryClient } from "@tanstack/react-query";
import type { DataRouter } from "react-router";
import { meKey } from "@/features/auth/api/auth";
import { authRedirectPaths, type AuthRedirectReason } from "@/shared/api/session";

/**
 * The in-app handler for an ended session, a sign-out or a pending password
 * change. Several failing requests can report the same reason at once, so a
 * redirect that is already on its way (or done) is not repeated.
 */
export function createAuthRedirectHandler(queryClient: QueryClient, router: DataRouter) {
  return (reason: AuthRedirectReason) => {
    const target = authRedirectPaths[reason];
    const targetPath = target.split("?")[0];
    const { location, navigation } = router.state;
    if (navigation.location?.pathname === targetPath) return;
    if (location.pathname === targetPath && reason !== "session_expired") return;
    if (location.pathname + location.search === target) return;

    if (reason === "password_change_required") {
      void queryClient.invalidateQueries({ queryKey: meKey });
    } else {
      // Nothing from the previous session may show to the next user.
      queryClient.clear();
    }
    void router.navigate(target, { replace: true });
  };
}
