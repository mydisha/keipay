import { useEffect, type ReactNode } from "react";
import { Navigate } from "react-router";
import { useMe, useSetupStatus } from "@/features/auth/api/auth";
import { AuthSkeleton } from "@/features/auth/components/auth-layout";
import { isApiError } from "@/shared/api/errors";
import { CHANGE_PASSWORD_PATH, redirectForAuth, type AuthRedirectReason } from "@/shared/api/session";
import { ErrorState } from "@/shared/ui/error-state";
import { ShellSkeleton } from "./layout/shell-skeleton";

function signedOutReason(error: unknown): AuthRedirectReason | null {
  if (!isApiError(error) || error.status !== 401) return null;
  return error.code === "session_expired" ? "session_expired" : "session_required";
}

function AuthRedirect({ reason, fallback }: { reason: AuthRedirectReason; fallback: ReactNode }) {
  useEffect(() => redirectForAuth(reason), [reason]);
  return fallback;
}

function FullPageError({ error, onRetry }: { error: unknown; onRetry: () => void }) {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-background px-4 py-10">
      <ErrorState error={error} onRetry={onRetry} className="w-full max-w-md" />
    </main>
  );
}

interface RequireAuthProps {
  children: ReactNode;
  /** The change-password page itself must stay reachable while a change is pending. */
  allowPasswordChange?: boolean;
  fallback?: ReactNode;
}

/** Renders protected pages only for a signed-in user who needs no password change. */
export function RequireAuth({ children, allowPasswordChange = false, fallback = <ShellSkeleton /> }: RequireAuthProps) {
  const me = useMe();
  const reason = signedOutReason(me.error);
  if (reason) return <AuthRedirect reason={reason} fallback={fallback} />;
  if (me.data) {
    if (me.data.user.must_change_password && !allowPasswordChange) {
      return <Navigate to={CHANGE_PASSWORD_PATH} replace />;
    }
    return children;
  }
  if (me.isError) return <FullPageError error={me.error} onRetry={() => void me.refetch()} />;
  return fallback;
}

/**
 * `/login` and `/setup`: a signed-in user goes to the console, and each page
 * sends the visitor to the other one when first-run setup says so.
 */
export function PublicOnly({ page, children }: { page: "login" | "setup"; children: ReactNode }) {
  const me = useMe();
  const setup = useSetupStatus();
  const signedIn = me.data !== undefined && signedOutReason(me.error) === null;

  if (signedIn) return <Navigate to={me.data.user.must_change_password ? CHANGE_PASSWORD_PATH : "/"} replace />;
  if (me.isPending || setup.isPending) return <AuthSkeleton />;
  if (page === "login" && setup.data?.setup_required) return <Navigate to="/setup" replace />;
  if (page === "setup" && setup.data?.setup_required === false) return <Navigate to="/login" replace />;
  return children;
}
