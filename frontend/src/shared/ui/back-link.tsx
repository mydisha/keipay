import { ChevronLeftIcon } from "lucide-react";
import { Link } from "react-router";

/** The "Back to …" link above a detail page's header. */
export function BackLink({ to, label }: { to: string; label: string }) {
  return (
    <Link
      to={to}
      className="inline-flex w-fit items-center gap-1 rounded-sm text-sm text-muted-foreground outline-none hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50 max-md:min-h-11"
    >
      <ChevronLeftIcon className="size-4" aria-hidden="true" />
      {label}
    </Link>
  );
}
