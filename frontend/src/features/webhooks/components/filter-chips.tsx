import { XIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import type { FilterChipGroup } from "@/shared/ui/data-table";

const ALL = "all";
type Environment = "test" | "live";

/** The environment chips (All, Test, Live); the group starts at the Test/Live mode, so "All" is a chip of its own. */
export function environmentChips(value: Environment | null, onChange: (value: Environment | null) => void): FilterChipGroup {
  return {
    id: "environment",
    label: strings.webhooks.environmentFilter,
    options: [
      { value: ALL, label: strings.common.all },
      { value: "test", label: strings.environments.test ?? "Test" },
      { value: "live", label: strings.environments.live ?? "Live" },
    ],
    value: value ?? ALL,
    onChange: (next) => onChange(next === "test" || next === "live" ? next : null),
  };
}

/** A filter that only arrives through a link (endpoint or event id), shown as a chip that removes it. */
export function RemovableChip({ label, onRemove }: { label: string; onRemove: () => void }) {
  return (
    <span className="inline-flex max-w-full min-w-0 items-center gap-1 rounded-sm border border-brand bg-brand-soft py-0.5 pr-0.5 pl-2.5 text-xs font-medium text-brand">
      <span className="truncate font-mono" title={label}>
        {label}
      </span>
      <Button
        variant="ghost"
        size="icon-sm"
        className="size-6 rounded-sm text-brand hover:bg-brand/10 hover:text-brand"
        onClick={onRemove}
        aria-label={strings.webhooks.removeFilter(label)}
      >
        <XIcon aria-hidden="true" className="size-3.5" />
      </Button>
    </span>
  );
}
