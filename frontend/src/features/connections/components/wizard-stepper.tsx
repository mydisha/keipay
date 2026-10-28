import { CheckIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

interface WizardStepperProps {
  steps: readonly string[];
  /** 1-based. */
  current: number;
}

/** Numbered steps with text labels; the current one is marked with `aria-current="step"`. */
export function WizardStepper({ steps, current }: WizardStepperProps) {
  return (
    <nav aria-label={strings.connections.stepsLabel}>
      <ol className="flex flex-col gap-2 md:flex-row md:flex-wrap md:items-center md:gap-x-4">
        {steps.map((label, index) => {
          const number = index + 1;
          const done = number < current;
          const active = number === current;
          return (
            <li
              key={label}
              aria-current={active ? "step" : undefined}
              className={cn("flex items-center gap-2 text-sm", active ? "font-medium text-foreground" : "text-muted-foreground")}
            >
              <span
                aria-hidden="true"
                className={cn(
                  "flex size-6 shrink-0 items-center justify-center rounded-full border text-xs tabular-nums",
                  active && "border-primary bg-primary text-primary-foreground",
                  done && "border-primary text-foreground",
                )}
              >
                {done ? <CheckIcon className="size-3.5" /> : number}
              </span>
              <span className="sr-only">{strings.connections.stepOf(number, steps.length)} </span>
              {label}
            </li>
          );
        })}
      </ol>
    </nav>
  );
}
