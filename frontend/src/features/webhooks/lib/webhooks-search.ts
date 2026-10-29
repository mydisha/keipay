import { useCallback, useEffect, useRef } from "react";
import { useSearchParams } from "react-router";
import { useMode, type Mode } from "@/shared/lib/mode";

// The Webhooks page keeps its tab, filters and open record in the query
// string (`/webhooks?tab=inbound&id=inb_…`): every path under /webhooks/ is a
// gateway callback URL, so the console can't use one.

export type WebhooksTab = "outbound" | "inbound";

const ALL_ENVIRONMENTS = "all";

export const deliveryPath = (id: string) => `/webhooks?tab=outbound&id=${encodeURIComponent(id)}`;
export const inboundPath = (id: string) => `/webhooks?tab=inbound&id=${encodeURIComponent(id)}`;
export const eventDeliveriesPath = (eventId: string) => `/webhooks?tab=outbound&event_id=${encodeURIComponent(eventId)}`;

/** Query-string state of the Webhooks page. */
export function useWebhooksSearch() {
  const [params, setParams] = useSearchParams();
  const { mode } = useMode();

  const tab: WebhooksTab = params.get("tab") === "inbound" ? "inbound" : "outbound";
  const id = params.get("id");
  const rawEnvironment = params.get("environment");
  // No `environment` in the URL means "follow the Test/Live switch".
  const environment: Mode | null =
    rawEnvironment === ALL_ENVIRONMENTS ? null : rawEnvironment === "test" || rawEnvironment === "live" ? rawEnvironment : mode;

  const update = useCallback(
    (change: (next: URLSearchParams) => void, replace = true) =>
      setParams(
        (current) => {
          const next = new URLSearchParams(current);
          change(next);
          return next;
        },
        { replace },
      ),
    [setParams],
  );

  // Flipping the switch puts the lists back on the new mode, like Apps and Connections.
  const lastMode = useRef(mode);
  useEffect(() => {
    if (lastMode.current === mode) return;
    lastMode.current = mode;
    update((next) => next.delete("environment"));
  }, [mode, update]);

  return {
    tab,
    id,
    environment,
    get: (key: string) => params.get(key),
    setTab: (value: WebhooksTab) =>
      update((next) => {
        const keep = next.get("environment");
        for (const key of [...next.keys()]) next.delete(key);
        next.set("tab", value);
        if (keep) next.set("environment", keep);
      }, false),
    setFilter: (key: string, value: string | null) =>
      update((next) => {
        if (value === null) next.delete(key);
        else next.set(key, value);
      }),
    setEnvironment: (value: Mode | null) => update((next) => next.set("environment", value ?? ALL_ENVIRONMENTS)),
    clearFilters: (keys: readonly string[]) =>
      update((next) => {
        for (const key of keys) next.delete(key);
        next.set("environment", ALL_ENVIRONMENTS);
      }),
    openDetail: (value: string) =>
      update((next) => {
        next.set("tab", tab);
        next.set("id", value);
      }, false),
    closeDetail: () => update((next) => next.delete("id")),
  };
}

export type WebhooksSearch = ReturnType<typeof useWebhooksSearch>;
