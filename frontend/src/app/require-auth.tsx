import type { ReactNode } from "react";
import { Navigate } from "react-router";
import { getToken } from "@/shared/api/session";

export function RequireAuth({ children }: { children: ReactNode }) {
  return getToken() ? children : <Navigate to="/login" replace />;
}
