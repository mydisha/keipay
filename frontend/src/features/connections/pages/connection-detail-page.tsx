import { Link, useParams } from "react-router";
import { toast } from "sonner";
import { usePermission } from "@/features/auth/components/can";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { BackLink } from "@/shared/ui/back-link";
import { Button } from "@/shared/ui/button";
import { Card, CardContent } from "@/shared/ui/card";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { PageHeader } from "@/shared/ui/page-header";
import { Section } from "@/shared/ui/section";
import { providerLabel, useConnection, useProviders } from "../api/connections";
import { ChannelsForm } from "../components/channels-form";
import { backToConnections, ConnectionHeader, ConnectionHeaderSkeleton } from "../components/connection-header";
import { ConnectionFacts, connectionHeadingId, CredentialHints, DetailsForm } from "../components/connection-sections";
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
        <BackLink {...backToConnections} />
        <PageHeader title={strings.connections.title} />
        {isNotFound(connection.error) ? (
          <EmptyState
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
      <div className="grid grid-cols-1 gap-6 lg:grid-cols-2 lg:items-start">
        <Section id={connectionHeadingId("overview")} title={strings.connections.factsTitle}>
          <ConnectionFacts connection={current} providerName={providerName} />
        </Section>
        <Section id={connectionHeadingId("callback")} title={strings.connections.callbackUrl} description={strings.connections.webhookBody}>
          <Card>
            <CardContent>
              <WebhookSetup connection={current} provider={provider} />
            </CardContent>
          </Card>
        </Section>
        <CredentialHints connection={current} provider={provider} canWrite={canWrite} />
        <Section id={connectionHeadingId("details")} title={strings.connections.detailsTitle} description={strings.connections.detailsBody}>
          <DetailsForm key={current.id} connection={current} provider={provider} readOnly={!canWrite} />
        </Section>
        <Section id={connectionHeadingId("channels")} title={strings.connections.channelsTitle} description={strings.connections.channelsBody}>
          <Card>
            <CardContent>
              <ChannelsForm
                key={current.id}
                connection={current}
                readOnly={!canWrite}
                onSaved={() => toast.success(strings.connections.channelsSaved)}
              />
            </CardContent>
          </Card>
        </Section>
      </div>
    </>
  );
}
