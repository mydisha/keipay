import { AlertTriangleIcon, RotateCwIcon } from "lucide-react";
import { errorMessage } from "@/shared/api/error-message";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Button } from "./button";

interface ErrorStateProps {
  error: unknown;
  onRetry?: () => void;
  title?: string;
  className?: string;
}

export function ErrorState({ error, onRetry, title = strings.errors.title, className }: ErrorStateProps) {
  return (
    <div
      role="alert"
      data-testid="error-state"
      className={cn(
        "flex flex-col items-center justify-center gap-3 rounded-lg border border-danger-border bg-danger px-6 py-10 text-center text-danger-foreground",
        className,
      )}
    >
      <AlertTriangleIcon className="size-5" aria-hidden="true" />
      <div className="max-w-md space-y-1">
        <p className="font-semibold">{title}</p>
        <p className="text-sm">{errorMessage(error)}</p>
      </div>
      {onRetry && (
        <Button variant="outline" size="sm" onClick={onRetry} className="text-foreground">
          <RotateCwIcon aria-hidden="true" />
          {strings.common.retry}
        </Button>
      )}
    </div>
  );
}
