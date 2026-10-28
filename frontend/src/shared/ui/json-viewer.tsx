import { ChevronRightIcon } from "lucide-react";
import { useId, useState } from "react";
import { strings } from "@/shared/lib/strings";
import { cn } from "@/shared/lib/utils";
import { CopyButton } from "./copy-button";

/** Longer payloads are cut in the view (copy still gets everything). */
export const JSON_PREVIEW_LIMIT = 20_000;

interface JsonViewerProps {
  value: unknown;
  label: string;
  defaultOpen?: boolean;
  className?: string;
}

export function stringifyJson(value: unknown): string {
  if (value === undefined) return "null";
  try {
    return JSON.stringify(value, null, 2) ?? "null";
  } catch {
    return String(value);
  }
}

export function JsonViewer({ value, label, defaultOpen = false, className }: JsonViewerProps) {
  const [open, setOpen] = useState(defaultOpen);
  const contentId = useId();
  const text = stringifyJson(value);
  const truncated = text.length > JSON_PREVIEW_LIMIT;
  const shown = truncated ? text.slice(0, JSON_PREVIEW_LIMIT) : text;
  const isEmpty = value === null || value === undefined;

  return (
    <div className={cn("rounded-md border bg-card", className)}>
      <div className="flex items-center justify-between gap-2 px-2 py-1">
        <button
          type="button"
          className="inline-flex min-h-8 items-center gap-1 rounded-sm px-1 text-sm font-medium outline-none focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11"
          aria-expanded={open}
          aria-controls={contentId}
          onClick={() => setOpen((o) => !o)}
        >
          <ChevronRightIcon
            className={cn("size-4 transition-transform", open && "rotate-90")}
            aria-hidden="true"
          />
          {label}
          {isEmpty && " "}
          {isEmpty && <span className="font-normal text-muted-foreground">({strings.json.empty})</span>}
        </button>
        <CopyButton value={text} label={strings.json.copyJson(label)} />
      </div>
      {open && (
        <div id={contentId} className="border-t">
          <pre className="max-h-80 overflow-auto p-3 font-mono text-xs leading-5 whitespace-pre">{shown}</pre>
          {truncated && (
            <p className="border-t px-3 py-2 text-xs text-muted-foreground">
              {strings.json.truncated(JSON_PREVIEW_LIMIT, text.length)}
            </p>
          )}
        </div>
      )}
    </div>
  );
}
