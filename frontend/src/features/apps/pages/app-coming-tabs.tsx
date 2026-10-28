import { RouteIcon, WebhookIcon } from "lucide-react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { useDetailApp } from "./app-detail-page";
import { WEBHOOK_URL_ANCHOR } from "./app-overview-tab";

// Placeholders for later phases: they explain what will live here and make no API calls.

export function AppWebhooksTab() {
  const app = useDetailApp();
  return (
    <EmptyState
      icon={WebhookIcon}
      title={strings.apps.webhooksEmptyTitle}
      description={strings.apps.webhooksEmptyBody}
      action={
        <Button asChild variant="outline">
          <Link to={`/apps/${app.id}#${WEBHOOK_URL_ANCHOR}`}>{strings.apps.webhooksEmptyAction}</Link>
        </Button>
      }
    />
  );
}

export function AppRoutingTab() {
  return (
    <EmptyState icon={RouteIcon} title={strings.apps.routingEmptyTitle} description={strings.apps.routingEmptyBody} />
  );
}
