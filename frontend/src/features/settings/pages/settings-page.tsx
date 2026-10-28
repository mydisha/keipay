import { KeyRoundIcon, UsersIcon } from "lucide-react";
import { Link } from "react-router";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { EmptyState } from "@/shared/ui/empty-state";
import { PageHeader } from "@/shared/ui/page-header";
import { AuditLogSection } from "../components/audit-log-section";

export function SettingsPage() {
  return (
    <>
      <PageHeader title={strings.settings.title} description={strings.settings.subtitle} />
      <AuditLogSection />
      <section className="grid gap-3" aria-labelledby="account-heading">
        <h2 id="account-heading" className="text-base font-semibold">
          {strings.settings.accountTitle}
        </h2>
        <Card className="flex flex-wrap items-center justify-between gap-3 p-4">
          <p className="text-sm text-muted-foreground">{strings.settings.accountBody}</p>
          <Button asChild variant="outline">
            <Link to="/change-password">
              <KeyRoundIcon aria-hidden="true" />
              {strings.shell.changePassword}
            </Link>
          </Button>
        </Card>
      </section>
      <section className="grid gap-3" aria-labelledby="users-heading">
        <h2 id="users-heading" className="text-base font-semibold">
          {strings.settings.usersTitle}
        </h2>
        <EmptyState
          icon={UsersIcon}
          headingLevel={3}
          title={strings.settings.usersComingTitle}
          description={strings.settings.usersComingBody}
        />
      </section>
    </>
  );
}
