import { useId } from "react";
import { strings } from "@/shared/lib/strings";
import { DATE_PRESETS, matchPreset, presetRange, type DateRange } from "@/shared/lib/wib-dates";
import { Chip } from "@/shared/ui/chip";
import { Input } from "@/shared/ui/input";
import { Label } from "@/shared/ui/label";
import { REPORT_INTERVALS, type ReportInterval } from "../api/reports";

interface DateRangeFilterProps {
  range: { from: string; to: string };
  onChange: (range: Partial<DateRange>) => void;
  /** Reference time for the presets. */
  now: Date;
  /** Shown under the inputs, which are then marked invalid. */
  error: string | null;
}

/** Preset chips and two WIB date inputs; both days are included in full. */
export function DateRangeFilter({ range, onChange, now, error }: DateRangeFilterProps) {
  const id = useId();
  const errorId = `${id}-error`;
  const active = matchPreset(range, now);
  return (
    <div className="grid gap-2">
      <div role="group" aria-label={strings.reconciliation.presetsLabel} className="flex flex-wrap items-center gap-1.5">
        <span className="text-xs font-medium text-muted-foreground">{strings.reconciliation.presetsLabel}</span>
        {DATE_PRESETS.map((preset) => (
          <Chip key={preset} pressed={active === preset} onClick={() => onChange(presetRange(preset, now))}>
            {strings.reconciliation.presets[preset]}
          </Chip>
        ))}
      </div>
      <div className="flex flex-wrap items-end gap-3">
        <DateInput
          id={`${id}-from`}
          label={strings.reconciliation.from}
          value={range.from}
          onChange={(from) => onChange({ from })}
          errorId={error ? errorId : undefined}
        />
        <DateInput
          id={`${id}-to`}
          label={strings.reconciliation.to}
          value={range.to}
          onChange={(to) => onChange({ to })}
          errorId={error ? errorId : undefined}
        />
      </div>
      {error && (
        <p id={errorId} role="alert" className="text-xs font-medium text-danger-foreground">
          {error}
        </p>
      )}
    </div>
  );
}

function DateInput({
  id,
  label,
  value,
  onChange,
  errorId,
}: {
  id: string;
  label: string;
  value: string;
  onChange: (value: string) => void;
  errorId: string | undefined;
}) {
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id} className="text-xs text-muted-foreground">
        {label}
      </Label>
      <Input
        id={id}
        type="date"
        value={value}
        onChange={(event) => onChange(event.target.value)}
        aria-invalid={errorId ? true : undefined}
        aria-describedby={errorId}
        className="w-44"
      />
    </div>
  );
}

/** Total / Day / Month. */
export function IntervalControl({ value, onChange }: { value: ReportInterval; onChange: (value: ReportInterval) => void }) {
  return (
    <div role="group" aria-label={strings.reconciliation.interval} className="flex flex-wrap items-center gap-1.5">
      <span className="text-xs font-medium text-muted-foreground">{strings.reconciliation.interval}</span>
      {REPORT_INTERVALS.map((interval) => (
        <Chip key={interval} pressed={value === interval} onClick={() => onChange(interval)}>
          {strings.reconciliation.intervals[interval]}
        </Chip>
      ))}
    </div>
  );
}
