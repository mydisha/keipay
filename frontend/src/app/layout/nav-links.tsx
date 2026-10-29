import { NavLink } from "react-router";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Separator } from "@/shared/ui/separator";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/shared/ui/tooltip";
import { navGroups, type NavItem } from "./nav";

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
          "relative flex h-9 items-center gap-3 rounded-md px-2.5 text-sm font-medium text-sidebar-foreground transition-colors outline-none before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full before:bg-sidebar-primary before:opacity-0 hover:bg-sidebar-accent hover:text-sidebar-accent-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11",
          isActive && "bg-sidebar-accent text-sidebar-primary before:opacity-100 hover:text-sidebar-primary",
          collapsed && "justify-center px-0",
        )
      }
    >
      <item.icon className="size-4 shrink-0" aria-hidden="true" />
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

/** The main navigation: one `nav`, its links grouped under small labels (a separator when collapsed). */
export function NavLinks({ collapsed = false, onNavigate }: NavLinksProps) {
  return (
    <nav aria-label={strings.nav.label} className="grid gap-3">
      {navGroups.map((group, index) => (
        <div key={group.label} className="grid gap-0.5">
          {collapsed ? (
            index > 0 && <Separator className="mx-auto mb-1 w-6" />
          ) : (
            <span aria-hidden="true" className="px-2.5 pb-1 text-[0.6875rem] font-medium tracking-wider text-muted-foreground uppercase">
              {group.label}
            </span>
          )}
          <ul className="grid gap-0.5">
            {group.items.map((item) => (
              <li key={item.to}>
                <NavItemLink item={item} collapsed={collapsed} onNavigate={onNavigate} />
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}
