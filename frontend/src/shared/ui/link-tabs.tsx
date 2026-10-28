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
      <ul className="inline-flex h-9 items-center gap-1 rounded-lg bg-muted p-1 text-muted-foreground max-md:h-[3.25rem]">
        {tabs.map((tab) => (
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
