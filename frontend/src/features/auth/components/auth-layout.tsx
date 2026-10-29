import { RouteIcon, ScaleIcon, ShieldCheckIcon, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { strings } from "@/shared/lib/strings";
import { Brand } from "@/shared/ui/brand";
import { Skeleton } from "@/shared/ui/skeleton";

const pointIcons: LucideIcon[] = [RouteIcon, ShieldCheckIcon, ScaleIcon];

/** The brand panel on the left of the auth pages; hidden below 1024px. */
function BrandPanel() {
  return (
    <section
      aria-label={strings.auth.brandPanelLabel}
      className="auth-brand-panel hidden flex-col justify-between p-10 text-brand-panel-foreground lg:flex xl:p-14"
    >
      <Brand tone="onPanel" />
      <div className="grid max-w-md gap-8">
        <div className="grid gap-3">
          <p className="text-3xl font-semibold tracking-tight xl:text-4xl">{strings.auth.brandTagline}</p>
          <p className="text-base text-brand-panel-muted">{strings.auth.brandSubtitle}</p>
        </div>
        <ul className="grid gap-4">
          {strings.auth.brandPoints.map((point, index) => {
            const Icon = pointIcons[index] ?? RouteIcon;
            return (
              <li key={point.title} className="flex items-start gap-3">
                <span className="flex size-8 shrink-0 items-center justify-center rounded-md bg-brand-panel-foreground/10">
                  <Icon className="size-4" aria-hidden="true" />
                </span>
                <span className="grid gap-0.5">
                  <span className="font-medium">{point.title}</span>
                  <span className="text-sm text-brand-panel-muted">{point.body}</span>
                </span>
              </li>
            );
          })}
        </ul>
      </div>
      <p className="text-xs text-brand-panel-muted">{strings.auth.brandFooter}</p>
    </section>
  );
}

/** Split screen: the brand panel on the left, the page's heading and form on the right (one column on phones). */
export function AuthLayout({ title, subtitle, children }: { title: string; subtitle: string; children: ReactNode }) {
  return (
    <main className="grid min-h-dvh bg-background lg:grid-cols-2">
      <BrandPanel />
      <div className="flex items-center justify-center px-4 py-10 md:px-8">
        <div className="grid w-full max-w-sm gap-8">
          <Brand className="lg:hidden" />
          <div className="grid gap-1">
            <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
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
    <main data-testid="auth-skeleton" aria-busy="true" className="grid min-h-dvh bg-background lg:grid-cols-2">
      <span role="status" className="sr-only">
        {strings.common.loading}
      </span>
      <div className="auth-brand-panel hidden lg:block" />
      <div className="flex items-center justify-center px-4 py-10 md:px-8">
        <div className="grid w-full max-w-sm gap-8">
          <Brand className="lg:hidden" />
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
