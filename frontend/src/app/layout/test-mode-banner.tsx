import { FlaskConicalIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";

export function TestModeBanner() {
  return (
    <div
      role="status"
      data-testid="test-mode-banner"
      className="flex items-start gap-2 border-b border-test-mode-border bg-test-mode px-4 py-1.5 text-xs text-test-mode-foreground md:px-6 lg:px-8"
    >
      <FlaskConicalIcon className="mt-0.5 size-3.5 shrink-0" aria-hidden="true" />
      <p>
        <span className="font-semibold">{strings.shell.testBannerTitle}.</span> {strings.shell.testBannerBody}
      </p>
    </div>
  );
}
