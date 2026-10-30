import { useSearchParams } from "react-router";
import { DEFAULT_RANGE, isDashboardRange, type DashboardRange } from "../api/dashboard";

/** The dashboard's period lives in `?range=` so a reload or shared link keeps it. */
export function useDashboardRange(): [DashboardRange, (range: DashboardRange) => void] {
  const [params, setParams] = useSearchParams();
  const raw = params.get("range");
  const range = isDashboardRange(raw) ? raw : DEFAULT_RANGE;
  const setRange = (next: DashboardRange) =>
    setParams(
      (current) => {
        const updated = new URLSearchParams(current);
        if (next === DEFAULT_RANGE) updated.delete("range");
        else updated.set("range", next);
        return updated;
      },
      { replace: true },
    );
  return [range, setRange];
}
