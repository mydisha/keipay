import { RouteIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { EmptyState } from "@/shared/ui/empty-state";

// Placeholder until this tab is built: it explains what will live here and makes no API calls.

export function AppRoutingTab() {
  return (
    <EmptyState icon={RouteIcon} title={strings.apps.routingEmptyTitle} description={strings.apps.routingEmptyBody} />
  );
}
