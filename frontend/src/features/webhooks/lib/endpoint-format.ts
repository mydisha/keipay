import { strings } from "@/shared/lib/strings";

/** The host of an endpoint URL, typed to confirm rotation and deletion. */
export function urlHost(url: string): string {
  try {
    return new URL(url).host || url;
  } catch {
    return url;
  }
}

/** A readable name for a subscription entry: `*` and `payment.*` get words, types stay as they are. */
export function subscriptionLabel(entry: string): string {
  if (entry === "*") return strings.endpoints.allEvents;
  if (entry === "payment.*") return strings.endpoints.allPaymentEvents;
  return entry;
}

export interface SubscriptionSummary {
  shown: string[];
  more: number;
}

/** Up to `limit` subscriptions in words, plus how many more there are. */
export function summarizeSubscriptions(events: string[], limit = 3): SubscriptionSummary {
  const labels = events.map(subscriptionLabel);
  return { shown: labels.slice(0, limit), more: Math.max(0, labels.length - limit) };
}

/** Whether a subscription list already covers `type` through a wildcard. */
export function coveredByWildcard(events: readonly string[], type: string): boolean {
  if (events.includes("*")) return true;
  return events.some((entry) => entry.endsWith(".*") && type.startsWith(entry.slice(0, -1)));
}
