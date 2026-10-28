import { Link } from "react-router";
import type { AppSummary } from "../api/apps";
import { EnvironmentBadge } from "./environment-badge";

/** An app in another feature's row: its name (linking to the app) and environment as text. */
export function AppLabel({ app }: { app: AppSummary }) {
  return (
    <span className="inline-flex max-w-full min-w-0 items-center gap-2">
      <Link
        to={`/apps/${app.id}`}
        title={app.name}
        className="truncate rounded-sm underline-offset-4 outline-none hover:underline focus-visible:ring-[3px] focus-visible:ring-ring/50"
        onClick={(event) => event.stopPropagation()}
      >
        {app.name}
      </Link>
      <EnvironmentBadge environment={app.environment} />
    </span>
  );
}
