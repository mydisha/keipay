import { PlugIcon } from "lucide-react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { PageHeader } from "@/shared/ui/page-header";

export function ConnectionsPage() {
  return (
    <>
      <PageHeader title={strings.connections.title} description={strings.connections.subtitle} />
      <EmptyState
        icon={PlugIcon}
        title={strings.connections.comingTitle}
        description={strings.connections.comingBody}
        action={
          <Button asChild>
            <Link to="/apps">{strings.connections.action}</Link>
          </Button>
        }
      />
    </>
  );
}
