import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/shared/ui/card";
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
      <section aria-labelledby={headingId}>
        <Card>
          <CardHeader>
            <CardTitle id={headingId}>{title}</CardTitle>
            {description && <CardDescription>{description}</CardDescription>}
          </CardHeader>
          <CardContent className="grid gap-4">{children}</CardContent>
        </Card>
      </section>
    </div>
  );
}
