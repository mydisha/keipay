import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import type { AppEnvironment } from "../api/apps";

/** `Test` or `Live` as text; test uses the amber test-mode tokens so it reads like the banner. */
export function EnvironmentBadge({ environment, className }: { environment: AppEnvironment; className?: string }) {
  return (
    <span
      data-environment={environment}
      className={cn(
        "inline-flex items-center rounded-full border px-2 py-0.5 text-xs font-medium whitespace-nowrap",
        environment === "test"
          ? "border-test-mode-border bg-test-mode text-test-mode-foreground"
          : "border-border bg-card text-foreground",
        className,
      )}
    >
      {strings.environments[environment] ?? environment}
    </span>
  );
}
