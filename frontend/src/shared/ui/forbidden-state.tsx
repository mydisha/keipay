import { LockIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";

interface ForbiddenStateProps {
  title?: string;
  description?: string;
  className?: string;
  /** Heading level for the title; pages use h2, nested sections h3. */
  headingLevel?: 2 | 3;
}

/** A page or section the signed-in user's role can't open. */
export function ForbiddenState({
  title = strings.forbidden.title,
  description = strings.forbidden.body,
  className,
  headingLevel = 2,
}: ForbiddenStateProps) {
  const Heading = headingLevel === 2 ? "h2" : "h3";
  return (
    <div
      data-testid="forbidden-state"
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-lg border bg-card px-6 py-12 text-center",
        className,
      )}
    >
      <span className="flex size-10 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <LockIcon className="size-5" aria-hidden="true" />
      </span>
      <div className="max-w-md space-y-1">
        <Heading className="text-base font-semibold">{title}</Heading>
        <p className="text-sm text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}
