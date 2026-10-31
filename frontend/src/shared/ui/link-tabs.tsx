import { NavLink } from "react-router";
import { cn } from "@/shared/lib/utils";
import { underlineTabClass } from "@/shared/ui/tabs";

export interface LinkTab {
  to: string;
  label: string;
}

/**
 * Tabs where each view has its own URL: links styled as tabs inside a labelled
 * `nav`; NavLink marks the active one with `aria-current="page"`.
 */
export function LinkTabs({ label, tabs }: { label: string; tabs: LinkTab[] }) {
  return (
    <nav aria-label={label} className="max-w-full overflow-x-auto">
      <ul className="flex items-end gap-1 border-b text-muted-foreground">
        {tabs.map((tab) => (
          <li key={tab.to} className="-mb-px">
            <NavLink
              to={tab.to}
              end
              className={({ isActive }) =>
                cn(
                  underlineTabClass,
                  isActive && "text-foreground after:opacity-100",
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
