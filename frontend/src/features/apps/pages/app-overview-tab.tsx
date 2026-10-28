import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Card } from "@/shared/ui/card";
import { DateTimeText } from "@/shared/ui/date-time-text";
import { JsonViewer } from "@/shared/ui/json-viewer";
import { StatusBadge } from "@/shared/ui/status-badge";
import { EnvironmentBadge } from "../components/environment-badge";
import { useDetailApp } from "./app-detail-page";

export const WEBHOOK_URL_ANCHOR = "webhook-url";

function Fact({ label, id, children }: { label: string; id?: string; children: ReactNode }) {
  return (
    <div id={id} className="grid min-w-0 gap-1 sm:grid-cols-[12rem_minmax(0,1fr)] sm:gap-4">
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0">{children}</dd>
    </div>
  );
}

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
      <section className="grid gap-3" aria-labelledby="app-details-heading">
        <h2 id="app-details-heading" className="text-base font-semibold">
          {strings.apps.detailsTitle}
        </h2>
        <Card className="p-4">
          <dl className="grid gap-3 text-sm">
            <Fact label={strings.apps.slug}>
              <span className="font-mono text-[0.8125rem]">{app.slug}</span>
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
            <Fact label={strings.apps.webhookUrl} id={WEBHOOK_URL_ANCHOR}>
              <UrlValue value={app.webhook_url} />
            </Fact>
            <Fact label={strings.apps.redirectSuccessUrl}>
              <UrlValue value={app.redirect_success_url} />
            </Fact>
            <Fact label={strings.apps.redirectFailureUrl}>
              <UrlValue value={app.redirect_failure_url} />
            </Fact>
          </dl>
        </Card>
      </section>
      <section className="grid gap-3" aria-labelledby="app-metadata-heading">
        <h2 id="app-metadata-heading" className="text-base font-semibold">
          {strings.apps.metadata}
        </h2>
        {hasMetadata ? (
          <JsonViewer value={app.metadata} label={strings.apps.metadata} defaultOpen />
        ) : (
          <p className="text-sm text-muted-foreground">{strings.apps.noMetadata}</p>
        )}
      </section>
    </div>
  );
}
