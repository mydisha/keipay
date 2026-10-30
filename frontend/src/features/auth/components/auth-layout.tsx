import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Brand } from "@/shared/ui/brand";
import { Skeleton } from "@/shared/ui/skeleton";
import { ThemeMenu } from "@/shared/ui/theme-menu";

/**
 * One sheet of paper on the desk: the product name and the theme button along
 * its top edge, then the page's heading and its form in the middle. Nothing
 * competes with the form. On a phone the sheet is the whole screen.
 */
function AuthSheet({ children, busy = false }: { children: ReactNode; busy?: boolean }) {
  return (
    <div className="flex min-h-dvh bg-sidebar sm:p-3">
      <div className="flex min-w-0 flex-1 flex-col bg-card sm:rounded-xl sm:border sm:shadow-xs">
        <header className="flex h-14 items-center justify-between gap-4 px-4 sm:px-6">
          <Brand />
          <ThemeMenu />
        </header>
        <main
          data-testid={busy ? "auth-skeleton" : undefined}
          aria-busy={busy || undefined}
          className="flex flex-1 items-center justify-center px-4 pt-6 pb-20 sm:px-6"
        >
          <div className="grid w-full max-w-sm gap-8">{children}</div>
        </main>
      </div>
    </div>
  );
}

export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <AuthSheet>
      <div className="grid gap-2 text-center">
        <h1 className="text-[1.75rem] leading-9 font-medium tracking-[-0.02em] text-balance">{title}</h1>
        <p className="text-sm text-pretty text-muted-foreground">{subtitle}</p>
      </div>
      {children}
    </AuthSheet>
  );
}

/** Placeholder for the sign-in, setup and password pages while their guard decides. */
export function AuthSkeleton() {
  return (
    <AuthSheet busy>
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="grid justify-items-center gap-3">
        <Skeleton className="h-8 w-48" />
        <Skeleton className="h-4 w-64 max-w-full" />
      </div>
      <div className="grid gap-4">
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
        <Skeleton className="h-9 w-full" />
      </div>
    </AuthSheet>
  );
}
