import { strings } from "@/shared/lib/strings";

/** One amber line under the top bar while the console is in test mode. */
export function TestModeBanner() {
  return (
    <div
      role="status"
      data-testid="test-mode-banner"
      className="flex min-h-8 items-center gap-2 border-b border-test-mode-border bg-test-mode px-4 py-1.5 text-xs text-test-mode-foreground md:px-6 lg:px-8"
    >
      <span className="shrink-0 font-semibold">{strings.shell.testBannerTitle}</span>
      <span aria-hidden="true" className="h-3 w-px shrink-0 bg-current opacity-40" />
      <p className="min-w-0">
        <span className="max-sm:hidden">{strings.shell.testBannerScope} </span>
        {strings.shell.testBannerPayments}
      </p>
    </div>
  );
}
