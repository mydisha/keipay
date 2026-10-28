import { CheckIcon, CopyIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { Button } from "./button";

const FEEDBACK_MS = 1500;

export async function copyText(value: string): Promise<boolean> {
  try {
    if (!navigator.clipboard?.writeText) throw new Error("clipboard unavailable");
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    toast.error(strings.common.copyFailed);
    return false;
  }
}

interface CopyButtonProps {
  value: string;
  /** Accessible name, e.g. "Copy API key". */
  label: string;
  className?: string;
  showText?: boolean;
}

export function CopyButton({ value, label, className, showText = false }: CopyButtonProps) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<number | undefined>(undefined);

  useEffect(() => () => window.clearTimeout(timer.current), []);

  async function onCopy() {
    if (!(await copyText(value))) return;
    setCopied(true);
    window.clearTimeout(timer.current);
    timer.current = window.setTimeout(() => setCopied(false), FEEDBACK_MS);
  }

  const Icon = copied ? CheckIcon : CopyIcon;
  return (
    <Button
      variant="ghost"
      size={showText ? "sm" : "icon-sm"}
      className={cn(!showText && "size-7", className)}
      onClick={(event) => {
        // Copying an id inside a clickable row must not also open the row.
        event.stopPropagation();
        void onCopy();
      }}
      aria-label={showText ? undefined : label}
      title={label}
    >
      <Icon aria-hidden="true" className={cn("size-3.5", copied && "text-success-foreground")} />
      {showText && <span>{copied ? strings.common.copied : strings.common.copy}</span>}
      <span role="status" aria-live="polite" className="sr-only">
        {copied ? strings.common.copied : ""}
      </span>
    </Button>
  );
}
