import { LayoutDashboardIcon } from "lucide-react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { EmptyState } from "@/shared/ui/empty-state";
import { PageHeader } from "@/shared/ui/page-header";

export function DashboardPage() {
  return (
    <>
      <PageHeader title={strings.dashboard.title} description={strings.dashboard.subtitle} />
      <EmptyState
        icon={LayoutDashboardIcon}
        title={strings.dashboard.comingTitle}
        description={strings.dashboard.comingBody}
        action={
          <Button asChild>
            <Link to="/payments">{strings.dashboard.action}</Link>
          </Button>
        }
      />
    </>
  );
}
