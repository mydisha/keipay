import { formatDateTimeWIB, formatRelative, parseDate } from "@/shared/lib/format";
import { strings } from "@/shared/lib/strings";
import { useNow } from "@/shared/lib/use-now";
import { cn } from "@/shared/lib/utils";
import { Tooltip, TooltipContent, TooltipTrigger } from "./tooltip";

interface DateTimeTextProps {
  value: string | Date | null | undefined;
  /** Reference time for the relative label; defaults to a clock that ticks each minute. */
  now?: Date;
  className?: string;
}

export function DateTimeText({ value, now, className }: DateTimeTextProps) {
  const clock = useNow();
  const date = parseDate(value);
  if (!date) return <span className={cn("text-muted-foreground", className)}>{strings.common.none}</span>;
  const absolute = formatDateTimeWIB(date);
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <time
          dateTime={date.toISOString()}
          tabIndex={0}
          aria-label={absolute}
          className={cn("whitespace-nowrap underline decoration-dotted decoration-muted-foreground/50 underline-offset-4", className)}
        >
          {formatRelative(date, now ?? clock)}
        </time>
      </TooltipTrigger>
      <TooltipContent>{absolute}</TooltipContent>
    </Tooltip>
  );
}
