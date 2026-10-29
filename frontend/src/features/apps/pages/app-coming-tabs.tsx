import { RouteIcon } from "lucide-react";
import { strings } from "@/shared/lib/strings";
import { EmptyState } from "@/shared/ui/empty-state";

// Placeholder for a later phase: it explains what will live here and makes no API calls.

export function AppRoutingTab() {
  return (
    <EmptyState icon={RouteIcon} title={strings.apps.routingEmptyTitle} description={strings.apps.routingEmptyBody} />
  );
}
