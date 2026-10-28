import { SearchXIcon } from "lucide-react";
import { Link, useParams } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { PageHeader } from "@/shared/ui/page-header";
import { providerLabel, useConnection, useProviders } from "../api/connections";
import { BackToConnections } from "../components/back-to-connections";
import { ChannelsForm } from "../components/channels-form";
import { ConnectionHeader, ConnectionHeaderSkeleton } from "../components/connection-header";
import { ConnectionFacts, CredentialHints, DetailsForm, Section } from "../components/connection-sections";
import { WebhookSetup } from "../components/webhook-setup";

function isNotFound(error: unknown): boolean {
  // A malformed id is a 400 (rule `public_id`); to the user it is just not there.
  return isApiError(error) && (error.code === "not_found" || error.code === "invalid_request");
}

/** `/connections/:id`: facts, callback URL, credential hints, settings and channels. */
export function ConnectionDetailPage() {
  const { id = "" } = useParams();
  const canWrite = usePermission("connections:write");
  const connection = useConnection(id);
  const providers = useProviders();

  if (connection.isPending || providers.isPending) {
    return (
      <>
        <ConnectionHeaderSkeleton />
        <span className="sr-only" aria-live="polite">
          {strings.common.loading}
        </span>
      </>
    );
  }

  const error = connection.error ?? providers.error;
  if (error || !connection.data) {
    return (
      <>
        <BackToConnections />
        <PageHeader title={strings.connections.title} />
        {isNotFound(connection.error) ? (
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
          <ErrorState
            error={error}
            onRetry={() => {
              if (connection.isError) void connection.refetch();
              if (providers.isError) void providers.refetch();
            }}
          />
        )}
      </>
    );
  }

  const current = connection.data;
  const provider = providers.data?.find((p) => p.name === current.provider);
  const providerName = providerLabel(providers.data, current.provider);

  return (
    <>
      <ConnectionHeader connection={current} providerName={providerName} />
      <div className="grid gap-6 lg:grid-cols-2 lg:items-start">
        <Section id="overview" title={strings.connections.factsTitle}>
          <ConnectionFacts connection={current} providerName={providerName} />
        </Section>
        <Section id="callback" title={strings.connections.callbackUrl} description={strings.connections.webhookBody}>
          <Card className="p-4">
            <WebhookSetup connection={current} provider={provider} />
          </Card>
        </Section>
        <CredentialHints connection={current} provider={provider} canWrite={canWrite} />
        <Section id="details" title={strings.connections.detailsTitle} description={strings.connections.detailsBody}>
          <DetailsForm key={current.id} connection={current} provider={provider} readOnly={!canWrite} />
        </Section>
        <Section id="channels" title={strings.connections.channelsTitle} description={strings.connections.channelsBody}>
          <Card className="p-4">
            <ChannelsForm
              key={current.id}
              connection={current}
              readOnly={!canWrite}
              onSaved={() => toast.success(strings.connections.channelsSaved)}
            />
          </Card>
        </Section>
      </div>
    </>
  );
}
