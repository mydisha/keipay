import { NavLink } from "react-router";
import { usePermissions } from "@/features/auth/components/can";
import type { Permission } from "@/features/auth/api/auth";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

interface SettingsTab {
  to: string;
  label: string;
  permission?: Permission;
}

const tabs: SettingsTab[] = [
  { to: "/settings", label: strings.settings.accountTab },
  { to: "/settings/users", label: strings.settings.usersTab, permission: "users:manage" },
  { to: "/settings/audit-log", label: strings.settings.auditTab, permission: "audit:read" },
];

/** Route-backed tabs; a tab the role can't open is not shown at all. */
export function SettingsTabs() {
  const can = usePermissions();
  const visible = tabs.filter((tab) => !tab.permission || can(tab.permission));

  return (
    <nav aria-label={strings.settings.tabsLabel} className="max-w-full overflow-x-auto">
      <ul className="inline-flex h-9 items-center gap-1 rounded-lg bg-muted p-1 text-muted-foreground max-md:h-[3.25rem]">
        {visible.map((tab) => (
          <li key={tab.to} className="h-full">
            <NavLink
              to={tab.to}
              end
              className={({ isActive }) =>
                cn(
                  "inline-flex h-full items-center justify-center rounded-md px-3 text-sm font-medium whitespace-nowrap transition-colors outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50",
                  isActive && "bg-card text-foreground shadow-sm",
                )
              }
            >
              {tab.label}
            </NavLink>
          </li>
        ))}
      </ul>
    </nav>
  );
}
