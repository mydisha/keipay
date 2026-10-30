import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Brand } from "@/shared/ui/brand";
import { Skeleton } from "@/shared/ui/skeleton";

/** One sheet of paper on the desk: the product name, then the page's heading and its form. Nothing competes with the form. */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <main className="flex min-h-dvh items-center justify-center bg-sidebar px-4 py-10">
      <div className="grid w-full max-w-sm gap-5">
        <Brand className="px-1" />
        <div className="grid gap-6 rounded-xl border bg-card p-6 shadow-xs md:p-8">
          <div className="grid gap-1">
            <h1 className="text-2xl font-medium tracking-[-0.02em]">{title}</h1>
            <p className="text-sm text-muted-foreground">{subtitle}</p>
          </div>
          {children}
        </div>
      </div>
    </main>
  );
}

/** Placeholder for the sign-in, setup and password pages while their guard decides. */
export function AuthSkeleton() {
  return (
    <main data-testid="auth-skeleton" aria-busy="true" className="flex min-h-dvh items-center justify-center bg-sidebar px-4 py-10">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="grid w-full max-w-sm gap-5">
        <Brand className="px-1" />
        <div className="grid gap-6 rounded-xl border bg-card p-6 shadow-xs md:p-8">
          <div className="grid gap-2">
            <Skeleton className="h-8 w-48" />
            <Skeleton className="h-4 w-64 max-w-full" />
          </div>
          <div className="grid gap-4">
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
            <Skeleton className="h-9 w-full" />
          </div>
        </div>
      </div>
    </main>
  );
}
