import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Brand } from "@/shared/ui/brand";
import { Card, CardContent } from "@/shared/ui/card";
import { Skeleton } from "@/shared/ui/skeleton";

export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-background px-4 py-10">
      <div className="grid w-full max-w-sm gap-6">
        <div className="grid justify-items-center gap-3 text-center">
          <Brand />
          <div className="space-y-1">
            <h1 className="text-xl font-semibold tracking-tight">{title}</h1>
            <p className="text-sm text-muted-foreground">{subtitle}</p>
          </div>
        </div>
        <Card>
          <CardContent className="p-5">{children}</CardContent>
        </Card>
      </div>
    </main>
  );
}

/** Placeholder for the sign-in, setup and password pages while their guard decides. */
export function AuthSkeleton() {
  return (
    <main data-testid="auth-skeleton" aria-busy="true" className="flex min-h-dvh items-center justify-center bg-background px-4 py-10">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="grid w-full max-w-sm gap-6">
        <div className="grid justify-items-center gap-3">
          <Brand />
          <Skeleton className="h-7 w-48" />
          <Skeleton className="h-4 w-64 max-w-full" />
        </div>
        <Card>
          <CardContent className="grid gap-4 p-5">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </CardContent>
        </Card>
      </div>
    </main>
  );
}
