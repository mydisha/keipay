import { NavLink } from "react-router";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { navItems } from "./nav";

interface NavLinksProps {
  collapsed?: boolean;
  onNavigate?: () => void;
}

export function NavLinks({ collapsed = false, onNavigate }: NavLinksProps) {
  return (
    <nav aria-label={strings.nav.label}>
      <ul className="grid gap-0.5">
        {navItems.map((item) => {
          const link = (
            <NavLink
              to={item.to}
              end={item.to === "/"}
              onClick={onNavigate}
              aria-label={collapsed ? item.label : undefined}
              className={({ isActive }) =>
                cn(
                  "flex h-9 items-center gap-3 rounded-md px-2.5 text-sm font-medium text-sidebar-foreground transition-colors outline-none hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11",
                  isActive && "bg-sidebar-accent text-sidebar-accent-foreground",
                  collapsed && "justify-center px-0",
                )
              }
            >
              <item.icon className="size-4 shrink-0" aria-hidden="true" />
              {!collapsed && <span className="truncate">{item.label}</span>}
            </NavLink>
          );
          return (
            <li key={item.to}>
              {collapsed ? (
                <Tooltip>
                  <TooltipTrigger asChild>{link}</TooltipTrigger>
                  <TooltipContent side="right">{item.label}</TooltipContent>
                </Tooltip>
              ) : (
                link
              )}
            </li>
          );
        })}
      </ul>
    </nav>
  );
}
