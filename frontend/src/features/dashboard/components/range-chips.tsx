import { strings } from "@/shared/lib/strings";
import { Chip } from "@/shared/ui/chip";
import { DASHBOARD_RANGES, type DashboardRange } from "../api/dashboard";

/** The period the dashboard covers: last 7 or 30 days. */
export function RangeChips({ value, onChange }: { value: DashboardRange; onChange: (range: DashboardRange) => void }) {
  return (
    <div role="group" aria-label={strings.dashboard.rangeLabel} className="flex flex-wrap items-center gap-1.5">
      {DASHBOARD_RANGES.map((range) => (
        <Chip key={range} pressed={value === range} onClick={() => onChange(range)}>
          {strings.dashboard.ranges[range] ?? range}
        </Chip>
      ))}
    </div>
  );
}
