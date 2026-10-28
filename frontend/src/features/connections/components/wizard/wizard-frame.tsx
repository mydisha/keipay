import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Card } from "@/shared/ui/card";
import { WizardStepper } from "../wizard-stepper";

export const WIZARD_STEPS = [
  strings.connections.stepProvider,
  strings.connections.stepCredentials,
  strings.connections.stepTest,
  strings.connections.stepWebhook,
  strings.connections.stepChannels,
] as const;

interface WizardFrameProps {
  /** 1-based step shown as current in the stepper. */
  step: number;
  title: string;
  description?: string;
  children: ReactNode;
}

/** The stepper, then one card with the step's `h2`, its text and its content. */
export function WizardFrame({ step, title, description, children }: WizardFrameProps) {
  const headingId = `wizard-step-${step}`;
  return (
    <div className="grid max-w-3xl gap-4">
      <WizardStepper steps={WIZARD_STEPS} current={step} />
      <Card className="grid gap-4 p-4 md:p-6">
        <section aria-labelledby={headingId} className="grid gap-4">
          <div className="grid gap-1">
            <h2 id={headingId} className="text-base font-semibold">
              {title}
            </h2>
            {description && <p className="text-sm text-muted-foreground">{description}</p>}
          </div>
          {children}
        </section>
      </Card>
    </div>
  );
}
