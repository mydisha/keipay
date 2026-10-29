import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import {
  app,
  APP_ID,
  CON_ID,
  connectionListItem,
  delivery,
  deliveryAttempt,
  deliveryDetail,
  DLV_ID,
  EVT_ID,
  eventCatalog,
  INB_ID,
  inboundDetail,
  inboundWebhook,
  me,
  page,
  PAY_ID,
  provider,
  WHE_ID,
} from "@/test/fixtures";
import { errorBody, json, mockApi, type Handler } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const DELIVERIES = "/admin/webhook-deliveries";
const INBOUND = "/admin/inbound-webhooks";

beforeEach(() => setViewport("desktop"));

function routes(extra: Record<string, Handler | object> = {}) {
  return mockApi({
    "GET /admin/apps": page([app()]),
    "GET /admin/event-types": eventCatalog(),
    "GET /admin/connections": page([connectionListItem()]),
    "GET /admin/providers": { data: [provider()] },
    [`GET ${DELIVERIES}`]: page([delivery()]),
    [`GET ${INBOUND}`]: page([inboundWebhook()]),
    ...extra,
  });
}

function lastQuery(api: ReturnType<typeof routes>, path: string): URLSearchParams {
  const requests = api.to("GET", path);
  return new URL(requests[requests.length - 1]!.url).searchParams;
}

describe("Outbound deliveries", () => {
  it("lists deliveries with app, endpoint, status, attempts, response and the Test badge", async () => {
    routes({
      [`GET ${DELIVERIES}`]: page([
        delivery(),
        delivery({
          id: "dlv_01JBKQ7Z8M4X2Y6V3N9T5R1W0Z",
          status: "PENDING",
          attempts: 0,
          max_attempts: 1,
          last_response_status: null,
          next_attempt_at: "2026-01-01T00:00:30Z",
          event: { id: EVT_ID, type: "payment.paid", test: true },
          endpoint: null,
          payment_id: null,
        }),
      ]),
    });
    renderApp("/webhooks");
    expect(await screen.findByRole("heading", { level: 1, name: "Webhooks" })).toBeInTheDocument();
    await screen.findByText("No endpoint");
    const rows = within(screen.getByRole("table", { name: "Outbound" })).getAllByRole("row").slice(1);
    expect(within(rows[0]!).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(rows[0]).toHaveTextContent("https://shop.example.com/hooks/keipay");
    expect(within(rows[0]!).getByText("Failed")).toBeInTheDocument();
    expect(rows[0]).toHaveTextContent("3/3");
    expect(rows[0]).toHaveTextContent("500");
    expect(within(rows[1]!).getByText("Pending")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Test event")).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("0/1");
  });

  it("filters by the Test/Live mode and lets All drop the environment", async () => {
    window.localStorage.setItem(storageKeys.mode, "test");
    const api = routes();
    const { user, router } = renderApp("/webhooks");
    await screen.findByText("https://shop.example.com/hooks/keipay");
    expect(lastQuery(api, DELIVERIES).get("environment")).toBe("test");
    const group = screen.getByRole("group", { name: "Filter by environment" });
    expect(within(group).getByRole("button", { name: "Test" })).toHaveAttribute("aria-pressed", "true");
    await user.click(within(group).getByRole("button", { name: "All" }));
    await waitFor(() => expect(lastQuery(api, DELIVERIES).has("environment")).toBe(false));
    expect(router.state.location.search).toContain("environment=all");
  });

  it("keeps chip filters in the query string and reads them back", async () => {
    const api = routes();
    const { user, router } = renderApp(`/webhooks?status=FAILED&event_type=payment.paid&app_id=${APP_ID}`);
    await screen.findByText("https://shop.example.com/hooks/keipay");
    const query = lastQuery(api, DELIVERIES);
    expect(query.get("status")).toBe("FAILED");
    expect(query.get("event_type")).toBe("payment.paid");
    expect(query.get("app_id")).toBe(APP_ID);
    expect(query.get("environment")).toBe("live");
    expect(
      within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: "Failed" }),
    ).toHaveAttribute("aria-pressed", "true");

    await user.click(
      within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: "Delivered" }),
    );
    await waitFor(() => expect(router.state.location.search).toContain("status=DELIVERED"));
    await waitFor(() => expect(lastQuery(api, DELIVERIES).get("status")).toBe("DELIVERED"));
    await user.click(
      within(screen.getByRole("group", { name: "Filter by event type" })).getByRole("button", { name: "payment.settled" }),
    );
    await waitFor(() => expect(lastQuery(api, DELIVERIES).get("event_type")).toBe("payment.settled"));
  });

  it("shows endpoint and event filters from links as removable chips", async () => {
    const api = routes();
    const { user } = renderApp(`/webhooks?tab=outbound&endpoint_id=${WHE_ID}&event_id=${EVT_ID}`);
    await screen.findByText("https://shop.example.com/hooks/keipay");
    expect(lastQuery(api, DELIVERIES).get("endpoint_id")).toBe(WHE_ID);
    expect(lastQuery(api, DELIVERIES).get("event_id")).toBe(EVT_ID);
    await user.click(screen.getByRole("button", { name: `Remove filter: Endpoint ${WHE_ID}` }));
    await waitFor(() => expect(lastQuery(api, DELIVERIES).has("endpoint_id")).toBe(false));
    expect(lastQuery(api, DELIVERIES).get("event_id")).toBe(EVT_ID);
  });

  it("loads the next cursor page with Load more", async () => {
    const api = routes({
      [`GET ${DELIVERIES}`]: (request: Request) =>
        new URL(request.url).searchParams.get("cursor") === "c2"
          ? json(200, page([delivery({ id: "dlv_01JBKQ7Z8M4X2Y6V3N9T5R1W0Z", endpoint: null })]))
          : json(200, page([delivery()], "c2")),
    });
    const { user } = renderApp("/webhooks");
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("No endpoint")).toBeInTheDocument();
    expect(lastQuery(api, DELIVERIES).get("cursor")).toBe("c2");
    expect(screen.queryByRole("button", { name: "Load more" })).not.toBeInTheDocument();
  });

  it("tells an empty log apart from an empty filter result", async () => {
    routes({ [`GET ${DELIVERIES}`]: page([]) });
    const { user } = renderApp("/webhooks");
    expect(await screen.findByRole("heading", { name: "No deliveries yet" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View apps" })).toHaveAttribute("href", "/apps");
    await user.click(
      within(screen.getByRole("group", { name: "Filter by status" })).getByRole("button", { name: "Delivered" }),
    );
    expect(await screen.findByRole("heading", { name: "No deliveries match these filters" })).toBeInTheDocument();
    await user.click(screen.getAllByRole("button", { name: "Clear filters" }).at(-1)!);
    expect(await screen.findByRole("heading", { name: "No deliveries yet" })).toBeInTheDocument();
  });

  it("shows ErrorState with retry", async () => {
    routes({ [`GET ${DELIVERIES}`]: () => json(500, errorBody("internal_error", "boom")) });
    renderApp("/webhooks");
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Try again" })).toBeInTheDocument();
  });

  it("shows ForbiddenState without deliveries:read", async () => {
    routes({ "GET /admin/me": me({}, ["apps:read"]) });
    renderApp("/webhooks");
    expect(await screen.findByText("You don't have access to this page")).toBeInTheDocument();
    expect(screen.queryByRole("tab", { name: "Outbound" })).not.toBeInTheDocument();
  });

  it("renders cards at mobile width and opens the detail from a card", async () => {
    setViewport("mobile");
    routes({ [`GET ${DELIVERIES}/${DLV_ID}`]: { delivery: deliveryDetail() } });
    const { user, router } = renderApp("/webhooks");
    const cards = await screen.findByTestId("data-cards");
    await user.click(within(cards).getByRole("button", { name: `View delivery ${DLV_ID}` }));
    expect(router.state.location.search).toContain(`id=${DLV_ID}`);
    expect(await screen.findByRole("dialog", { name: "Delivery" })).toBeInTheDocument();
  });
});

describe("Delivery detail", () => {
  const PATH = `/webhooks?tab=outbound&id=${DLV_ID}`;

  it("shows ids, facts, payload and the attempt timeline with redacted headers", async () => {
    routes({
      [`GET ${DELIVERIES}/${DLV_ID}`]: {
        delivery: deliveryDetail({
          attempt_log: [
            deliveryAttempt(),
            deliveryAttempt({
              attempt: 2,
              manual: true,
              response_status: null,
              response_body: "x".repeat(10),
              response_body_truncated: true,
              error: "context deadline exceeded",
            }),
          ],
        }),
      },
    });
    const { user } = renderApp(PATH);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    expect(await within(sheet).findByRole("button", { name: `Copy Delivery ID ${DLV_ID}` })).toBeInTheDocument();
    expect(within(sheet).getByRole("button", { name: `Copy Event ID ${EVT_ID}` })).toBeInTheDocument();
    expect(within(sheet).getByRole("button", { name: `Copy Payment ID ${PAY_ID}` })).toBeInTheDocument();
    expect(within(sheet).getByRole("button", { name: `Copy Endpoint ID ${WHE_ID}` })).toBeInTheDocument();
    expect(within(sheet).getByText("Failed")).toBeInTheDocument();

    const payload = within(sheet).getByRole("button", { name: "Payload" });
    expect(payload).toHaveAttribute("aria-expanded", "false");
    await user.click(payload);
    expect(within(sheet).getByText(/"api_version": "v2"/)).toBeInTheDocument();

    expect(within(sheet).getByText("Attempt 1")).toBeInTheDocument();
    expect(within(sheet).getByText("HTTP 500")).toBeInTheDocument();
    expect(within(sheet).getByText("Attempt 2")).toBeInTheDocument();
    expect(within(sheet).getByText("No response")).toBeInTheDocument();
    expect(within(sheet).getByText("context deadline exceeded")).toBeInTheDocument();
    expect(within(sheet).getAllByText("Replay").length).toBeGreaterThan(0);
    expect(within(sheet).getAllByText("[REDACTED]")[0]).toBeInTheDocument();
    expect(within(sheet).getByText("Only the first 4096 bytes of the response were kept.")).toBeInTheDocument();
  });

  it("says when no attempt was made yet and disables Replay while pending", async () => {
    routes({
      [`GET ${DELIVERIES}/${DLV_ID}`]: {
        delivery: deliveryDetail({ status: "PENDING", attempts: 0, attempt_log: [], next_attempt_at: "2026-01-01T00:00:30Z" }),
      },
    });
    renderApp(PATH);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    expect(await within(sheet).findByText("No attempts yet")).toBeInTheDocument();
    const replay = within(sheet).getByRole("button", { name: "Replay" });
    expect(replay).toBeDisabled();
    expect(replay).toHaveAccessibleDescription("A pending delivery can't be replayed until its attempts finish.");
  });

  it("replays after a confirm", async () => {
    const api = routes({
      [`GET ${DELIVERIES}/${DLV_ID}`]: { delivery: deliveryDetail() },
      [`POST ${DELIVERIES}/${DLV_ID}/replay`]: () => json(202, { delivery: delivery({ status: "PENDING" }) }),
    });
    const { user } = renderApp(PATH);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    await user.click(await within(sheet).findByRole("button", { name: "Replay" }));
    const dialog = await screen.findByRole("dialog", { name: "Replay this delivery?" });
    await user.click(within(dialog).getByRole("button", { name: "Replay" }));
    expect(await screen.findByText("Delivery queued for one more attempt")).toBeInTheDocument();
    expect(api.to("POST", `${DELIVERIES}/${DLV_ID}/replay`)).toHaveLength(1);
  });

  it.each([
    ["in_progress", "The delivery is still pending. Wait for its current attempt to finish."],
    ["no_endpoint", "This delivery has no endpoint to send to."],
    ["endpoint_deleted", "The endpoint was deleted, so this delivery can't be sent again."],
    ["endpoint_disabled", "The endpoint is disabled. Enable it on the app's Webhooks tab, then replay."],
  ])("explains a 409 %s", async (reason, message) => {
    routes({
      [`GET ${DELIVERIES}/${DLV_ID}`]: { delivery: deliveryDetail() },
      [`POST ${DELIVERIES}/${DLV_ID}/replay`]: () =>
        json(409, errorBody("delivery_not_replayable", "delivery not replayable", { reason })),
    });
    const { user } = renderApp(PATH);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    await user.click(await within(sheet).findByRole("button", { name: "Replay" }));
    await user.click(within(await screen.findByRole("dialog", { name: "Replay this delivery?" })).getByRole("button", { name: "Replay" }));
    expect(await screen.findByText(message)).toBeInTheDocument();
  });

  it("shows Delivery not found for an unknown id and closes back to the list", async () => {
    routes();
    const { user, router } = renderApp(`/webhooks?tab=outbound&id=dlv_nope`);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    expect(await within(sheet).findByText("Delivery not found")).toBeInTheDocument();
    await user.click(within(sheet).getByRole("button", { name: "Close" }));
    await waitFor(() => expect(router.state.location.search).not.toContain("id="));
  });

  it("shows ErrorState for other failures", async () => {
    routes({ [`GET ${DELIVERIES}/${DLV_ID}`]: () => json(500, errorBody("internal_error", "boom")) });
    renderApp(PATH);
    const sheet = await screen.findByRole("dialog", { name: "Delivery" });
    expect(await within(sheet).findByTestId("error-state")).toBeInTheDocument();
  });

  it("opens from a row click", async () => {
    routes({ [`GET ${DELIVERIES}/${DLV_ID}`]: { delivery: deliveryDetail() } });
    const { user, router } = renderApp("/webhooks");
    await user.click(await screen.findByText("https://shop.example.com/hooks/keipay"));
    expect(router.state.location.search).toBe(`?tab=outbound&id=${DLV_ID}`);
    expect(await screen.findByRole("dialog", { name: "Delivery" })).toBeInTheDocument();
  });
});

describe("Inbound webhooks", () => {
  it("lists callbacks with result and signature as text, filtered by mode", async () => {
    const api = routes({
      [`GET ${INBOUND}`]: page([
        inboundWebhook(),
        inboundWebhook({
          id: "inb_01JBKQ7Z8M4X2Y6V3N9T5R1W0Z",
          signature_valid: false,
          result: "invalid_signature",
          event_key: null,
          provider_status: null,
          reference: null,
          payment_id: null,
          event: null,
        }),
      ]),
    });
    const { user } = renderApp("/webhooks?tab=inbound");
    await screen.findByText("inv_123");
    const table = screen.getByRole("table", { name: "Inbound" });
    expect(within(table).getByText("Invalid signature")).toBeInTheDocument();
    expect(within(table).getByText("Verified")).toBeInTheDocument();
    expect(within(table).getByText("Invalid")).toBeInTheDocument();
    expect(within(table).getByText("Applied")).toBeInTheDocument();
    expect(within(table).getAllByText("xendit-test")).toHaveLength(2);
    expect(within(table).getAllByText("Xendit").length).toBeGreaterThan(0);
    expect(within(table).getByText("inv_123")).toBeInTheDocument();
    expect(lastQuery(api, INBOUND).get("environment")).toBe("live");
    expect(api.to("GET", DELIVERIES)).toHaveLength(0);

    await user.click(
      within(screen.getByRole("group", { name: "Filter by result" })).getByRole("button", { name: "Error" }),
    );
    await waitFor(() => expect(lastQuery(api, INBOUND).get("result")).toBe("error"));
  });

  it("labels all eight results", async () => {
    routes();
    renderApp("/webhooks?tab=inbound");
    const group = await screen.findByRole("group", { name: "Filter by result" });
    expect(within(group).getAllByRole("button").map((b) => b.textContent)).toEqual([
      "Applied",
      "Stale",
      "Duplicate",
      "Mismatch",
      "Unmapped",
      "Invalid signature",
      "Invalid payload",
      "Error",
    ]);
  });

  it("filters by connection", async () => {
    const api = routes();
    const { user } = renderApp("/webhooks?tab=inbound");
    await screen.findByText("inv_123");
    await user.click(screen.getByRole("combobox", { name: "Connection" }));
    await user.click(await screen.findByRole("option", { name: "xendit-test (Test)" }));
    await waitFor(() => expect(lastQuery(api, INBOUND).get("connection_id")).toBe(CON_ID));
  });

  it("switches tabs through the query string", async () => {
    routes();
    const { user, router } = renderApp("/webhooks?status=FAILED");
    await screen.findByText("https://shop.example.com/hooks/keipay");
    await user.click(screen.getByRole("tab", { name: "Inbound" }));
    await waitFor(() => expect(router.state.location.search).toBe("?tab=inbound"));
    expect(await screen.findByText("inv_123")).toBeInTheDocument();
  });

  it("shows the empty state and the filtered empty state", async () => {
    routes({ [`GET ${INBOUND}`]: page([]) });
    renderApp("/webhooks?tab=inbound");
    expect(await screen.findByRole("heading", { name: "No gateway webhooks received yet" })).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View connections" })).toHaveAttribute("href", "/connections");
  });

  it("shows the filtered empty state", async () => {
    routes({ [`GET ${INBOUND}`]: page([]) });
    renderApp("/webhooks?tab=inbound&result=stale");
    expect(await screen.findByRole("heading", { name: "No gateway webhooks match these filters" })).toBeInTheDocument();
  });

  it("shows a JSON raw body in the viewer with links to the connection and the event's deliveries", async () => {
    routes({ [`GET ${INBOUND}/${INB_ID}`]: { inbound_webhook: inboundDetail() } });
    renderApp(`/webhooks?tab=inbound&id=${INB_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Inbound webhook" });
    expect(await within(sheet).findByText(/"status": "PAID"/)).toBeInTheDocument();
    expect(within(sheet).getByText("Applied")).toBeInTheDocument();
    expect(within(sheet).getByText("Verified")).toBeInTheDocument();
    expect(within(sheet).getByRole("link", { name: "xendit-test" })).toHaveAttribute("href", `/connections/${CON_ID}`);
    expect(within(sheet).getByRole("link", { name: "View deliveries" })).toHaveAttribute(
      "href",
      `/webhooks?tab=outbound&event_id=${EVT_ID}`,
    );
    expect(within(sheet).queryByText(/stored cut to 4096 bytes/)).not.toBeInTheDocument();
  });

  it("shows a non-JSON raw body as text with the truncated note and the result detail", async () => {
    routes({
      [`GET ${INBOUND}/${INB_ID}`]: {
        inbound_webhook: inboundDetail({
          result: "invalid_signature",
          signature_valid: false,
          result_detail: "callback token mismatch",
          raw_body: "not json <xml/>",
          raw_body_truncated: true,
          event: null,
        }),
      },
    });
    renderApp(`/webhooks?tab=inbound&id=${INB_ID}`);
    const sheet = await screen.findByRole("dialog", { name: "Inbound webhook" });
    expect(await within(sheet).findByTestId("raw-body-text")).toHaveTextContent("not json <xml/>");
    expect(within(sheet).getByText("Invalid signature")).toBeInTheDocument();
    expect(within(sheet).getByText("Invalid")).toBeInTheDocument();
    expect(within(sheet).getByText("callback token mismatch")).toBeInTheDocument();
    expect(within(sheet).getByText("Bodies that fail verification are stored cut to 4096 bytes.")).toBeInTheDocument();
  });

  it("shows not found for an unknown inbound id", async () => {
    routes();
    renderApp("/webhooks?tab=inbound&id=inb_nope");
    const sheet = await screen.findByRole("dialog", { name: "Inbound webhook" });
    expect(await within(sheet).findByText("Inbound webhook not found")).toBeInTheDocument();
  });

  it("renders cards at mobile width", async () => {
    setViewport("mobile");
    routes();
    renderApp("/webhooks?tab=inbound");
    const cards = await screen.findByTestId("data-cards");
    expect(within(cards).getByRole("button", { name: `View inbound webhook ${INB_ID}` })).toBeInTheDocument();
  });
});
