import { SearchXIcon } from "lucide-react";
import { Link, Outlet, useOutletContext, useParams } from "react-router";
import { isApiError } from "@/shared/api/errors";
import { strings } from "@/shared/lib/strings";
import { BackLink } from "@/shared/ui/back-link";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { ErrorState } from "@/shared/ui/error-state";
import { LinkTabs } from "@/shared/ui/link-tabs";
import { PageHeader } from "@/shared/ui/page-header";
import { useApp, type App } from "../api/apps";
import { AppHeader, AppHeaderSkeleton, backToApps } from "../components/app-header";

/** The loaded app, for the tab routes under `/apps/:id`. */
export function useDetailApp(): App {
  return useOutletContext<App>();
}

function isNotFound(error: unknown): boolean {
  // A malformed id is a 400 (rule `public_id`); to the user it is just not there.
  return isApiError(error) && (error.code === "not_found" || error.code === "invalid_request");
}

/** `/apps/:id`: header, route-backed tabs and the active tab. */
export function AppDetailPage() {
  const { id = "" } = useParams();
  const app = useApp(id);

  if (app.isPending) {
    return (
      <>
        <AppHeaderSkeleton />
        <span className="sr-only" aria-live="polite">
          {strings.common.loading}
        </span>
      </>
    );
  }

  if (app.isError) {
    return (
      <>
        <BackLink {...backToApps} />
        <PageHeader title={strings.apps.title} />
        {isNotFound(app.error) ? (
          <EmptyState
            icon={SearchXIcon}
            title={strings.apps.notFoundTitle}
            description={strings.apps.notFoundBody}
            action={
              <Button asChild variant="outline">
                <Link to="/apps">{strings.apps.backToApps}</Link>
              </Button>
            }
          />
        ) : (
          <ErrorState error={app.error} onRetry={() => void app.refetch()} />
        )}
      </>
    );
  }

  const base = `/apps/${app.data.id}`;
  return (
    <>
      <AppHeader app={app.data} />
      <LinkTabs
        label={strings.apps.tabsLabel}
        tabs={[
          { to: base, label: strings.apps.overviewTab },
          { to: `${base}/keys`, label: strings.apps.keysTab },
          { to: `${base}/webhooks`, label: strings.apps.webhooksTab },
          { to: `${base}/routing`, label: strings.apps.routingTab },
        ]}
      />
      <Outlet context={app.data} />
    </>
  );
}
