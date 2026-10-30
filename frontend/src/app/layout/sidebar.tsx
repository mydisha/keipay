import { PanelLeftCloseIcon, PanelLeftOpenIcon } from "lucide-react";
import { useState } from "react";
import { readStorage, storageKeys, writeStorage } from "@/shared/lib/storage";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Button } from "@/shared/ui/button";
import { Brand } from "@/shared/ui/brand";
import { NavLinks } from "./nav-links";

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
      <div className={cn("flex h-14 shrink-0 items-center gap-2", collapsed ? "justify-center px-2" : "pr-2 pl-4.5")}>
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
      <div className="flex-1 overflow-y-auto px-2 pt-2 pb-3">
        <NavLinks collapsed={collapsed} />
      </div>
    </aside>
  );
}
