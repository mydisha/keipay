import { CircleAlertIcon, Loader2Icon, RotateCwIcon, TriangleAlertIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { Alert, AlertDescription } from "@/shared/ui/alert";
import { Button } from "@/shared/ui/button";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { StatusBadge } from "@/shared/ui/status-badge";
import { useTestConnection, type Connection, type Provider } from "../../api/connections";
import { ChannelsForm } from "../channels-form";
import { ReplaceCredentialsForm } from "../replace-credentials-form";
import { WebhookSetup } from "../webhook-setup";
import { WizardFrame } from "./wizard-frame";

interface StepProps {
  connection: Connection;
  provider: Provider | undefined;
  onContinue: () => void;
  onSkip: () => void;
}

function SkipButton({ onSkip }: { onSkip: () => void }) {
  return (
    <Button variant="ghost" onClick={onSkip}>
      {strings.connections.skip}
    </Button>
  );
}

interface TestOutcome {
  result: "ok" | "failed";
  message: string | null;
  testedAt: string | null;
}

/** The latest result: this session's test, else what the connection stored. */
function outcomeOf(connection: Connection, latest: TestOutcome | null): TestOutcome | null {
  if (latest) return latest;
  if (!connection.last_test_status) return null;
  return {
    result: connection.last_test_status,
    message: connection.last_test_error,
    testedAt: connection.last_tested_at,
  };
}

/** Step 3: tests the new connection once by itself, then on request. */
export function TestStep({ connection, provider, onContinue, onSkip }: StepProps) {
  const test = useTestConnection();
  const [editing, setEditing] = useState(false);
  const [latest, setLatest] = useState<TestOutcome | null>(null);
  // One automatic run for an untested connection, also under StrictMode's double effects.
  const autoTested = useRef(false);

  function runTest() {
    setLatest(null);
    test.mutate(connection.id, {
      onSuccess: (data) => setLatest({ result: data.result, message: data.message, testedAt: data.tested_at }),
    });
  }

  useEffect(() => {
    if (autoTested.current || connection.last_test_status !== null) return;
    autoTested.current = true;
    runTest();
    // Once per mounted step: later runs are the user's (Test again, Save and test).
  }, []);

  if (editing) {
    return (
      <WizardFrame step={2} title={strings.connections.editCredentials} description={strings.connections.replaceBody}>
        <ReplaceCredentialsForm
          connection={connection}
          fields={provider?.credential_schema.fields ?? []}
          submitLabel={strings.connections.saveAndTest}
          secondaryActions={
            <Button variant="outline" onClick={() => setEditing(false)}>
              {strings.common.cancel}
            </Button>
          }
          onSaved={() => {
            setEditing(false);
            runTest();
          }}
        />
      </WizardFrame>
    );
  }

  const outcome = test.isPending ? null : outcomeOf(connection, latest);
  return (
    <WizardFrame step={3} title={strings.connections.testTitle} description={strings.connections.testBody}>
      <div className="grid gap-3" aria-live="polite">
        {test.isPending && (
          <p className="flex items-center gap-2 text-sm">
            <Loader2Icon className="size-4 animate-spin" aria-hidden="true" />
            {strings.connections.testing}
          </p>
        )}
        {test.isError && (
          <Alert variant="danger">
            <CircleAlertIcon aria-hidden="true" />
            <AlertDescription>{errorMessage(test.error, strings.connections.testRequestFailed)}</AlertDescription>
          </Alert>
        )}
        {outcome && (
          <div className="grid gap-2" data-testid="test-outcome">
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <StatusBadge status={outcome.result} />
              {outcome.message && <span>{outcome.message}</span>}
            </div>
            {outcome.testedAt && (
              <p className="text-xs text-muted-foreground">
                {strings.connections.testedAt} <DateTimeText value={outcome.testedAt} />
              </p>
            )}
            {outcome.result === "failed" && (
              <Alert variant="warning">
                <TriangleAlertIcon aria-hidden="true" />
                <AlertDescription>{strings.connections.testFailedHint}</AlertDescription>
              </Alert>
            )}
          </div>
        )}
        {!test.isPending && !test.isError && !outcome && (
          <p className="text-sm text-muted-foreground">{strings.connections.notTestedYet}</p>
        )}
      </div>
      <div className="flex flex-wrap justify-end gap-2">
        <SkipButton onSkip={onSkip} />
        <Button variant="outline" onClick={() => setEditing(true)} disabled={test.isPending}>
          {strings.connections.editCredentials}
        </Button>
        <Button variant="outline" onClick={runTest} disabled={test.isPending}>
          <RotateCwIcon aria-hidden="true" />
          {strings.connections.testAgain}
        </Button>
        <Button onClick={onContinue}>{strings.connections.continue}</Button>
      </div>
    </WizardFrame>
  );
}

/** Step 4: the callback URL to paste into the gateway dashboard. */
export function WebhookStep({ connection, provider, onContinue, onSkip }: StepProps) {
  return (
    <WizardFrame step={4} title={strings.connections.webhookTitle} description={strings.connections.webhookBody}>
      <WebhookSetup connection={connection} provider={provider} />
      <div className="flex flex-wrap justify-end gap-2">
        <SkipButton onSkip={onSkip} />
        <Button onClick={onContinue}>{strings.connections.continue}</Button>
      </div>
    </WizardFrame>
  );
}

/** Step 5: which channels the connection offers; saving finishes the wizard. */
export function ChannelsStep({
  connection,
  onSaved,
  onSkip,
}: {
  connection: Connection;
  onSaved: (connection: Connection) => void;
  onSkip: () => void;
}) {
  return (
    <WizardFrame step={5} title={strings.connections.channelsTitle} description={strings.connections.channelsBody}>
      <ChannelsForm connection={connection} onSaved={onSaved} extraActions={<SkipButton onSkip={onSkip} />} />
    </WizardFrame>
  );
}
