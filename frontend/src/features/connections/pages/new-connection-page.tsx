import { SearchXIcon } from "lucide-react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { toast } from "sonner";
import { RequirePermission } from "@/features/auth/components/can";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { PageHeader } from "@/shared/ui/page-header";
import { Skeleton } from "@/shared/ui/skeleton";
import { useConnection, useProviders, type Connection } from "../api/connections";
import { BackToConnections } from "../components/back-to-connections";
import { CreateConnectionSteps } from "../components/wizard/create-steps";
import { ChannelsStep, TestStep, WebhookStep } from "../components/wizard/post-create-steps";
import { WIZARD_STEPS } from "../components/wizard/wizard-frame";

const FIRST_RESUMABLE_STEP = 3;
const LAST_STEP = WIZARD_STEPS.length;

/** The step in `?step=`, kept to the steps that exist after creation. */
function resumeStep(value: string | null): number {
  const step = Number(value);
  if (!Number.isInteger(step) || step < FIRST_RESUMABLE_STEP) return FIRST_RESUMABLE_STEP;
  return Math.min(step, LAST_STEP);
}

/** `/connections/new`: the add-connection wizard. Writers only. */
export function NewConnectionPage() {
  return (
    <>
      <BackToConnections />
      <PageHeader title={strings.connections.wizardTitle} description={strings.connections.wizardSubtitle} />
      <RequirePermission permission="connections:write">
        <Wizard />
      </RequirePermission>
    </>
  );
}

function WizardSkeleton() {
  return (
    <div className="grid gap-4" aria-busy="true" data-testid="wizard-skeleton">
      <Skeleton className="h-6 w-full max-w-xl" />
      <Skeleton className="h-24 w-full max-w-2xl" />
      <Skeleton className="h-24 w-full max-w-2xl" />
      <span className="sr-only" aria-live="polite">
        {strings.common.loading}
      </span>
    </div>
  );
}

function Wizard() {
  const [params, setParams] = useSearchParams();
  const navigate = useNavigate();
  const providers = useProviders();
  const connectionId = params.get("connection");
  const connection = useConnection(connectionId);

  if (providers.isPending || (connectionId !== null && connection.isPending)) return <WizardSkeleton />;
  if (providers.isError) return <ErrorState error={providers.error} onRetry={() => void providers.refetch()} />;

  const goTo = (id: string, step: number) => setParams({ connection: id, step: String(step) });
  const openDetail = (id: string) => void navigate(`/connections/${id}`);

  if (connectionId === null) {
    if (providers.data.length === 0) {
      return <EmptyState icon={SearchXIcon} title={strings.connections.noProviders} />;
    }
    return (
      <CreateConnectionSteps
        providers={providers.data}
        initialEnvironment={params.get("environment")}
        onCreated={(created: Connection) => {
          toast.success(strings.connections.created_toast(created.name));
          goTo(created.id, FIRST_RESUMABLE_STEP);
        }}
      />
    );
  }

  if (connection.isError) {
    const notFound = isApiError(connection.error) && ["not_found", "invalid_request"].includes(connection.error.code);
    return notFound ? (
      <EmptyState
        icon={SearchXIcon}
        title={strings.connections.notFoundTitle}
        description={strings.connections.notFoundBody}
        action={
          <Button asChild variant="outline">
            <Link to="/connections">{strings.connections.backToConnections}</Link>
          </Button>
        }
      />
    ) : (
      <ErrorState error={connection.error} onRetry={() => void connection.refetch()} />
    );
  }
  if (!connection.data) return <WizardSkeleton />;

  const current = connection.data;
  const provider = providers.data.find((p) => p.name === current.provider);
  const step = resumeStep(params.get("step"));

  if (step === 3) {
    return (
      <TestStep
        connection={current}
        provider={provider}
        onContinue={() => goTo(current.id, 4)}
        onSkip={() => openDetail(current.id)}
      />
    );
  }
  if (step === 4) {
    return (
      <WebhookStep
        connection={current}
        provider={provider}
        onContinue={() => goTo(current.id, 5)}
        onSkip={() => openDetail(current.id)}
      />
    );
  }
  return (
    <ChannelsStep
      connection={current}
      onSaved={(saved) => {
        toast.success(strings.connections.ready_toast(saved.name));
        openDetail(saved.id);
      }}
      onSkip={() => openDetail(current.id)}
    />
  );
}
