import type { ReactNode } from "react";
import { ForbiddenState } from "@/shared/ui/forbidden-state";
import { useMe, type Permission } from "../api/auth";

// Components ask for a permission, never a role: `GET /admin/me` lists what
// the role allows, and the server enforces it anyway.

/** A checker for several permissions at once (for lists of tabs or actions). */
export function usePermissions(): (permission: Permission) => boolean {
  const { data } = useMe();
  const granted = data?.permissions;
  return (permission) => granted?.includes(permission) ?? false;
}

export function usePermission(permission: Permission): boolean {
  return usePermissions()(permission);
}

/** Renders its children only when the signed-in user has the permission. */
export function Can({ permission, children }: { permission: Permission; children: ReactNode }) {
  return usePermission(permission) ? children : null;
}

/** For whole pages and tabs: shows ForbiddenState instead of hiding them. */
export function RequirePermission({ permission, children }: { permission: Permission; children: ReactNode }) {
  return usePermission(permission) ? children : <ForbiddenState />;
}
