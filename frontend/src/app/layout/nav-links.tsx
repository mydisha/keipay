import { NavLink } from "react-router";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { settingsItem, workItems, type NavItem } from "./nav";

interface NavLinksProps {
  collapsed?: boolean;
  onNavigate?: () => void;
}

function NavItemLink({ item, collapsed, onNavigate }: { item: NavItem; collapsed: boolean; onNavigate?: () => void }) {
  const link = (
    <NavLink
      to={item.to}
      end={item.to === "/"}
      onClick={onNavigate}
      aria-label={collapsed ? item.label : undefined}
      className={({ isActive }) =>
        cn(
          "group/nav flex h-9 items-center gap-3 rounded-md px-2.5 text-sm text-muted-foreground transition-colors outline-none hover:text-sidebar-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11",
          isActive && "bg-sidebar-accent font-medium text-sidebar-accent-foreground",
          collapsed && "justify-center px-0",
        )
      }
    >
      <item.icon className="size-4 shrink-0 group-aria-[current=page]/nav:text-sidebar-primary" strokeWidth={1.75} aria-hidden="true" />
      {!collapsed && <span className="truncate">{item.label}</span>}
    </NavLink>
  );
  if (!collapsed) return link;
  return (
    <Tooltip>
      <TooltipTrigger asChild>{link}</TooltipTrigger>
      <TooltipContent side="right">{item.label}</TooltipContent>
    </Tooltip>
  );
}

/** The main navigation: the working pages, then Settings apart at the bottom of the same `nav`. */
export function NavLinks({ collapsed = false, onNavigate }: NavLinksProps) {
  return (
    <nav aria-label={strings.nav.label} className="flex h-full flex-col justify-between gap-6">
      <ul className="grid gap-0.5">
        {workItems.map((item) => (
          <li key={item.to}>
            <NavItemLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
          </li>
        ))}
      </ul>
      <ul className="grid gap-0.5">
        <li>
          <NavItemLink item={settingsItem} collapsed={collapsed} onNavigate={onNavigate} />
        </li>
      </ul>
    </nav>
  );
}
