import { useQuery } from "@tanstack/react-query";
import { api, unwrap } from "@/shared/api/client";
import type { components } from "@/shared/api/schema";

export type EventType = components["schemas"]["EventType"];
export type EventTypeInfo = components["schemas"]["EventTypeInfo"];
export type EventWildcard = components["schemas"]["EventWildcard"];
export type EventCatalog = components["schemas"]["EventTypeListResponse"];

export const eventTypeKeys = { all: ["event-types"] as const };

/** The event catalog; fixed per server version, so it is loaded once. */
export function useEventTypes() {
  return useQuery({
    queryKey: eventTypeKeys.all,
    queryFn: () => unwrap(api.GET("/admin/event-types")),
    staleTime: Infinity,
  });
}
