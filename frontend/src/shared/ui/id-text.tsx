import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { CopyButton } from "./copy-button";

interface IdTextProps {
  value: string | number;
  /** Shown before numeric ids, e.g. "#". Not copied. */
  prefix?: string;
  /** What the id is, for the copy button's accessible name ("payment id"). */
  label?: string;
  copyable?: boolean;
  className?: string;
}

/** Monospace identifier, truncated with the full value in `title`, with copy. */
export function IdText({ value, prefix = "", label = "ID", copyable = true, className }: IdTextProps) {
  const text = String(value);
  return (
    <span className={cn("inline-flex max-w-full min-w-0 items-center gap-1", className)}>
      <span className="truncate font-mono text-[0.8125rem]" title={text}>
        {prefix}
        {text}
      </span>
      {copyable && <CopyButton value={text} label={`${strings.common.copyValue(label)} ${text}`} />}
    </span>
  );
}
