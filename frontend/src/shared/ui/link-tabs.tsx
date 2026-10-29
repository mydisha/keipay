import { NavLink } from "react-router";
import { cn } from "@/shared/lib/utils";

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
                  "relative inline-flex h-10 items-center justify-center rounded-t-md px-3 text-sm font-medium whitespace-nowrap transition-colors outline-none after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:rounded-full after:bg-primary after:opacity-0 after:transition-opacity hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:h-11",
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
