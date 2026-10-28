import { KeyRoundIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { useMe } from "@/features/auth/api/auth";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card } from "@/shared/ui/card";
import { ErrorState } from "@/shared/ui/error-state";
import { Skeleton } from "@/shared/ui/skeleton";

export function AccountTab() {
  const me = useMe();

  let details: ReactNode;
  if (me.data) {
    const { user } = me.data;
    details = (
      <dl className="grid grid-cols-[minmax(0,auto)_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
        <dt className="text-muted-foreground">{strings.settings.name}</dt>
        <dd className="min-w-0 break-words">{user.name}</dd>
        <dt className="text-muted-foreground">{strings.settings.email}</dt>
        <dd className="min-w-0 break-all">{user.email}</dd>
        <dt className="text-muted-foreground">{strings.settings.role}</dt>
        <dd>{strings.roles[user.role] ?? user.role}</dd>
      </dl>
    );
  } else if (me.isError) {
    details = <ErrorState error={me.error} onRetry={() => void me.refetch()} />;
  } else {
    details = (
      <div className="grid gap-2" aria-busy="true" data-testid="account-skeleton">
        <Skeleton className="h-4 w-48" />
        <Skeleton className="h-4 w-64 max-w-full" />
        <Skeleton className="h-4 w-24" />
      </div>
    );
  }

  return (
    <section className="grid gap-3" aria-labelledby="account-heading">
      <div className="space-y-1">
        <h2 id="account-heading" className="text-base font-semibold">
          {strings.settings.accountTitle}
        </h2>
        <p className="text-sm text-muted-foreground">{strings.settings.accountBody}</p>
      </div>
      <Card className="grid gap-4 p-4">
        {details}
        <div className="flex flex-wrap items-center justify-between gap-3 border-t pt-4">
          <p className="text-sm text-muted-foreground">{strings.settings.accountPasswordBody}</p>
          <Button asChild variant="outline">
            <Link to="/change-password">
              <KeyRoundIcon aria-hidden="true" />
              {strings.shell.changePassword}
            </Link>
          </Button>
        </div>
      </Card>
    </section>
  );
}
