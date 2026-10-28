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
        "sticky top-0 hidden h-dvh shrink-0 flex-col gap-4 border-r border-sidebar-border bg-sidebar p-3 transition-[width] md:flex",
        collapsed ? "w-16" : "w-60",
      )}
    >
      <div className={cn("flex h-9 items-center", collapsed ? "justify-center" : "px-1.5")}>
        <Brand compact={collapsed} />
      </div>
      <div className="flex-1 overflow-y-auto">
        <NavLinks collapsed={collapsed} />
      </div>
      <Button
        variant="ghost"
        size="icon"
        onClick={toggle}
        aria-label={collapsed ? strings.nav.expand : strings.nav.collapse}
        aria-expanded={!collapsed}
        className={cn(!collapsed && "self-end")}
      >
        {collapsed ? <PanelLeftOpenIcon aria-hidden="true" /> : <PanelLeftCloseIcon aria-hidden="true" />}
      </Button>
    </aside>
  );
}
