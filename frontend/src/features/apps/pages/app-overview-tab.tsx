import { strings } from "@/shared/lib/strings";
import { Card, CardContent } from "@/shared/ui/card";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { Fact, FactList } from "@/shared/ui/fact-list";
import { JsonViewer } from "@/shared/ui/json-viewer";
import { Section } from "@/shared/ui/section";
import { StatusBadge } from "@/shared/ui/status-badge";
import { DefaultConnection } from "../components/default-connection";
import { EnvironmentBadge } from "../components/environment-badge";
import { useDetailApp } from "./app-detail-page";

function UrlValue({ value }: { value: string }) {
  if (!value) return <span className="text-muted-foreground">{strings.common.notSet}</span>;
  return (
    <span className="block truncate font-mono text-[0.8125rem]" title={value}>
      {value}
    </span>
  );
}

/** `/apps/:id` index tab: the app's settings and metadata. */
export function AppOverviewTab() {
  const app = useDetailApp();
  const hasMetadata = Object.keys(app.metadata).length > 0;

  return (
    <div className="grid gap-6">
      <Section id="app-details-heading" title={strings.apps.detailsTitle}>
        <Card>
          <CardContent>
            <FactList>
              <Fact label={strings.apps.slug} mono>
                {app.slug}
              </Fact>
              <Fact label={strings.apps.environment}>
                <EnvironmentBadge environment={app.environment} />
              </Fact>
              <Fact label={strings.apps.status}>
                <StatusBadge status={app.status} />
              </Fact>
              <Fact label={strings.apps.created}>
                <DateTimeText value={app.created_at} />
              </Fact>
              <Fact label={strings.apps.updated}>
                <DateTimeText value={app.updated_at} />
              </Fact>
              <Fact label={strings.apps.redirectSuccessUrl}>
                <UrlValue value={app.redirect_success_url} />
              </Fact>
              <Fact label={strings.apps.redirectFailureUrl}>
                <UrlValue value={app.redirect_failure_url} />
              </Fact>
            </FactList>
          </CardContent>
        </Card>
      </Section>
      <Section id="app-connection-heading" title={strings.apps.defaultConnection} description={strings.apps.defaultConnectionBody}>
        <Card>
          <CardContent className="text-sm">
            <DefaultConnection app={app} />
          </CardContent>
        </Card>
      </Section>
      <Section id="app-metadata-heading" title={strings.apps.metadata}>
        {hasMetadata ? (
          <JsonViewer value={app.metadata} label={strings.apps.metadata} defaultOpen />
        ) : (
          <p className="text-sm text-muted-foreground">{strings.apps.noMetadata}</p>
        )}
      </Section>
    </div>
  );
}
