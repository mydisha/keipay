import { CircleAlertIcon, RotateCwIcon } from "lucide-react";
import { useId } from "react";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { Checkbox } from "@/shared/ui/checkbox";
import { Label } from "@/shared/ui/label";
import { Skeleton } from "@/shared/ui/skeleton";
import { useEventTypes, type EventCatalog } from "../api/event-types";
import { coveredByWildcard, subscriptionLabel } from "../lib/endpoint-format";

interface Option {
  value: string;
  label: string;
  description: string;
  mono: boolean;
}

function options(catalog: EventCatalog): Option[] {
  return [
    ...catalog.wildcards.map((w) => ({
      value: w.pattern,
      label: subscriptionLabel(w.pattern),
      description: w.description,
      mono: false,
    })),
    ...catalog.data.map((t) => ({ value: t.type, label: t.type, description: t.description, mono: true })),
  ];
}

/** The next subscription list after ticking or unticking one option. */
export function toggleSubscription(current: readonly string[], value: string, checked: boolean): string[] {
  if (!checked) return current.filter((entry) => entry !== value);
  if (value === "*") return ["*"];
  if (value.endsWith(".*")) {
    const group = value.slice(0, -1);
    return [...current.filter((entry) => !entry.startsWith(group)), value];
  }
  return current.includes(value) ? [...current] : [...current, value];
}

interface EventsPickerProps {
  value: string[];
  onChange: (value: string[]) => void;
  disabled?: boolean;
  /** Field error to show under the options. */
  error?: string;
}

/**
 * Subscriptions as checkboxes, built from `GET /admin/event-types`. Ticking a
 * wildcard clears and disables the types it already covers.
 */
export function EventsPicker({ value, onChange, disabled = false, error }: EventsPickerProps) {
  const catalog = useEventTypes();
  const id = useId();
  const hintId = `${id}-hint`;
  const errorId = `${id}-error`;

  let body;
  if (catalog.isPending) {
    body = (
      <div className="grid gap-2" aria-busy="true">
        <span className="sr-only">{strings.endpoints.catalogLoading}</span>
        {Array.from({ length: 4 }, (_, i) => (
          <Skeleton key={i} data-testid="events-skeleton" className="h-9 w-full" />
        ))}
      </div>
    );
  } else if (catalog.isError) {
    body = (
      <Alert variant="danger">
        <CircleAlertIcon aria-hidden="true" />
        <AlertDescription className="flex flex-wrap items-center justify-between gap-2">
          <span>{strings.endpoints.catalogFailed}</span>
          <Button variant="outline" size="sm" className="text-foreground" onClick={() => void catalog.refetch()}>
            <RotateCwIcon aria-hidden="true" />
            {strings.common.retry}
          </Button>
        </AlertDescription>
      </Alert>
    );
  } else {
    body = (
      <ul className="grid gap-1">
        {options(catalog.data).map((option) => {
          const checked = value.includes(option.value);
          const covered = !checked && option.value !== "*" && coveredByWildcard(value, option.value);
          const optionId = `${id}-${option.value}`;
          return (
            <li key={option.value} className="flex items-start gap-3 rounded-md px-1 py-1.5 max-md:min-h-11">
              <Checkbox
                id={optionId}
                checked={checked || covered}
                disabled={disabled || covered}
                aria-describedby={`${optionId}-description`}
                onCheckedChange={(next) => onChange(toggleSubscription(value, option.value, next === true))}
                className="mt-0.5"
              />
              <div className="grid min-w-0 gap-0.5">
                <Label htmlFor={optionId} className={cn(option.mono && "font-mono text-[0.8125rem]")}>
                  {option.label}
                </Label>
                <p id={`${optionId}-description`} className="text-xs text-muted-foreground">
                  {option.description}
                </p>
              </div>
            </li>
          );
        })}
      </ul>
    );
  }

  return (
    <fieldset
      className="grid gap-2"
      aria-describedby={error ? `${hintId} ${errorId}` : hintId}
      aria-invalid={error ? true : undefined}
    >
      <legend className={cn("mb-1 text-sm font-medium", error && "text-danger-foreground")}>{strings.endpoints.events}</legend>
      <p id={hintId} className="-mt-1 text-xs text-muted-foreground">
        {strings.endpoints.eventsHint}
      </p>
      {body}
      {error && (
        <p id={errorId} className="text-xs font-medium text-danger-foreground">
          {error}
        </p>
      )}
    </fieldset>
  );
}
