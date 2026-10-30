import { KeyRoundIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Link } from "react-router";
import { useMe } from "@/features/auth/api/auth";
import { strings } from "@/shared/lib/strings";
import { Button } from "@/shared/ui/button";
import { Card, CardContent, CardFooter } from "@/shared/ui/card";
import { ErrorState } from "@/shared/ui/error-state";
import { Fact, FactList } from "@/shared/ui/fact-list";
import { Section } from "@/shared/ui/section";
import { Skeleton } from "@/shared/ui/skeleton";

export function AccountTab() {
  const me = useMe();

  let details: ReactNode;
  if (me.data) {
    const { user } = me.data;
    details = (
      <FactList labelWidth="sm">
        <Fact label={strings.settings.name}>
          <span className="break-words">{user.name}</span>
        </Fact>
        <Fact label={strings.settings.email}>
          <span className="break-all">{user.email}</span>
        </Fact>
        <Fact label={strings.settings.role}>{strings.roles[user.role] ?? user.role}</Fact>
      </FactList>
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
    <Section id="account-heading" title={strings.settings.accountTitle}>
      <Card>
        <CardContent>{details}</CardContent>
        <CardFooter className="justify-between">
          <p className="text-sm text-muted-foreground">{strings.settings.accountPasswordBody}</p>
          <Button asChild variant="outline">
            <Link to="/change-password">
              <KeyRoundIcon aria-hidden="true" />
              {strings.shell.changePassword}
            </Link>
          </Button>
        </CardFooter>
      </Card>
    </Section>
  );
}
