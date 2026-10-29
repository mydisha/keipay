import { PanelLeftCloseIcon, PanelLeftOpenIcon } from "lucide-react";
import { useState } from "react";
import { useMe } from "@/features/auth/api/auth";
import { readStorage, storageKeys, writeStorage } from "@/shared/lib/storage";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import { Brand } from "@/shared/ui/brand";
import { NavLinks } from "./nav-links";

/** The first letters of up to two words of a name. */
export function initialsOf(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((word) => word[0]?.toUpperCase() ?? "")
    .join("");
}

/** Who is signed in, as plain text; the account menu lives in the top bar. */
function SidebarIdentity({ collapsed }: { collapsed: boolean }) {
  const { data: me } = useMe();
  if (!me) return null;
  const roleLabel = strings.roles[me.user.role] ?? me.user.role;
  return (
    <div
      data-testid="sidebar-identity"
      className={cn("flex items-center gap-2.5 border-t border-sidebar-border pt-3", collapsed && "justify-center")}
    >
      <span
        aria-hidden="true"
        className="flex size-8 shrink-0 items-center justify-center rounded-full bg-sidebar-primary/15 text-xs font-semibold text-sidebar-primary"
      >
        {initialsOf(me.user.name)}
      </span>
      <span className={cn("grid min-w-0 gap-0.5", collapsed && "sr-only")}>
        <span className="truncate text-sm font-medium" title={me.user.name}>
          {me.user.name}
        </span>
        <span className="truncate text-xs text-muted-foreground">{roleLabel}</span>
      </span>
    </div>
  );
}

/** Desktop sidebar (768px and up); its collapsed state is remembered per browser. */
export function Sidebar() {
  const [collapsed, setCollapsed] = useState(() => readStorage(storageKeys.sidebarCollapsed) === "true");

  function toggle() {
    setCollapsed((value) => {
      writeStorage(storageKeys.sidebarCollapsed, String(!value));
      return !value;
    });
  }

  return (
    <aside
      data-collapsed={collapsed}
      className={cn(
        "sticky top-0 hidden h-dvh shrink-0 flex-col border-r border-sidebar-border bg-sidebar transition-[width] md:flex",
        collapsed ? "w-16" : "w-64",
      )}
    >
      <div className={cn("flex h-14 shrink-0 items-center gap-2 border-b border-sidebar-border", collapsed ? "justify-center px-2" : "px-4")}>
        {!collapsed && <Brand className="min-w-0 flex-1" />}
        <Button
          variant="ghost"
          size="icon-sm"
          onClick={toggle}
          aria-label={collapsed ? strings.nav.expand : strings.nav.collapse}
          aria-expanded={!collapsed}
        >
          {collapsed ? <PanelLeftOpenIcon aria-hidden="true" /> : <PanelLeftCloseIcon aria-hidden="true" />}
        </Button>
      </div>
      <div className="flex-1 overflow-y-auto px-2 py-3">
        <NavLinks collapsed={collapsed} />
      </div>
      <div className="px-3 pb-3">
        <SidebarIdentity collapsed={collapsed} />
      </div>
    </aside>
  );
}
