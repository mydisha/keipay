import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import {
  app,
  APP_ID,
  delivery,
  DLV_ID,
  eventCatalog,
  me,
  page,
  WEBHOOK_SECRET,
  webhookEndpoint,
  WHE_ID,
} from "@/test/fixtures";
import { errorBody, json, mockApi, type Handler } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const DETAIL = `GET /admin/apps/${APP_ID}`;
const LIST = `GET /admin/apps/${APP_ID}/webhook-endpoints`;
const CREATE = `POST /admin/apps/${APP_ID}/webhook-endpoints`;
const ITEM = `/admin/webhook-endpoints/${WHE_ID}`;
const PATH = `/apps/${APP_ID}/webhooks`;

beforeEach(() => setViewport("desktop"));

function routes(extra: Record<string, Handler | object> = {}) {
  return mockApi({
    [DETAIL]: { app: app() },
    "GET /admin/event-types": eventCatalog(),
    [LIST]: page([webhookEndpoint()]),
    ...extra,
  });
}

async function openMenu(user: ReturnType<typeof renderApp>["user"], url = webhookEndpoint().url) {
  await user.click(await screen.findByRole("button", { name: `Actions for ${url}` }));
}

describe("App webhooks tab: list and states", () => {
  it("shows skeleton rows while loading", async () => {
    routes({ [LIST]: () => new Promise<Response>(() => {}) });
    renderApp(PATH);
    expect((await screen.findAllByTestId("skeleton-row")).length).toBeGreaterThan(0);
  });

  it("shows the empty state with Add endpoint for writers", async () => {
    routes({ [LIST]: page([]) });
    renderApp(PATH);
    expect(await screen.findByRole("heading", { name: "No webhook endpoints yet" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Add endpoint" })).toHaveLength(2);
  });

  it("shows a read-only empty state without apps:write", async () => {
    routes({ "GET /admin/me": me({ role: "viewer" }), [LIST]: page([]) });
    renderApp(PATH);
    expect(await screen.findByText("Endpoints appear here once an owner or admin adds them.")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add endpoint" })).not.toBeInTheDocument();
  });

  it("shows ErrorState and retries", async () => {
    let calls = 0;
    routes({
      [LIST]: () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, page([webhookEndpoint()]));
      },
    });
    const { user } = renderApp(PATH);
    expect(await screen.findByTestId("error-state")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("https://shop.example.com/hooks/keipay")).toBeInTheDocument();
  });

  it("shows ForbiddenState for a 403", async () => {
    routes({ [LIST]: () => json(403, errorBody("forbidden", "forbidden")) });
    renderApp(PATH);
    expect(await screen.findByText("You don't have access to this page")).toBeInTheDocument();
  });

  it("lists endpoints with subscriptions, version, badges, status and secret hint", async () => {
    routes({
      [LIST]: page(
        [
          webhookEndpoint(),
          webhookEndpoint({
            id: "whe_01JBKQ7Z8M4X2Y6V3N9T5R1W0Z",
            url: "https://legacy.example.com/kei",
            description: "",
            events: ["*"],
            api_version: "v1",
            legacy_signature: true,
            migrated: true,
            secret_last4: null,
          }),
          webhookEndpoint({
            id: "whe_01JBKQ7Z8M4X2Y6V3N9T5R1W0Y",
            url: "https://many.example.com/kei",
            events: ["payment.paid", "payment.settled", "payment.expired", "payment.failed", "payment.duplicate"],
          }),
        ],
        "next",
      ),
    });
    renderApp(PATH);
    await screen.findByText("https://legacy.example.com/kei");
    const table = screen.getByRole("table", { name: "Webhook endpoints" });
    const rows = within(table).getAllByRole("row").slice(1);
    expect(rows).toHaveLength(3);
    expect(rows[0]).toHaveTextContent("Order service");
    expect(rows[0]).toHaveTextContent("payment.paid,payment.settled");
    expect(rows[0]).toHaveTextContent("v2");
    expect(rows[0]).toHaveTextContent("…c0de");
    expect(within(rows[0]!).getByText("Active")).toBeInTheDocument();
    expect(rows[1]).toHaveTextContent("All events");
    expect(within(rows[1]!).getByText("v1")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Legacy signature")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Migrated")).toBeInTheDocument();
    expect(within(rows[1]!).getByText("Migrated secret")).toBeInTheDocument();
    expect(rows[2]).toHaveTextContent("+2");
    expect(screen.getByRole("button", { name: "Load more" })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Copy Endpoint URL" })).toHaveLength(3);
  });

  it("warns about an automatically disabled endpoint and enables it", async () => {
    const disabled = webhookEndpoint({
      status: "disabled",
      disabled_reason: "consecutive_failures",
      disabled_at: "2026-01-02T03:04:05Z",
      consecutive_failures: 5,
    });
    const api = routes({
      [LIST]: page([disabled]),
      [`POST ${ITEM}/enable`]: { endpoint: webhookEndpoint() },
    });
    const { user } = renderApp(PATH);
    const warning = await screen.findByText(
      "Disabled automatically after 5 failed deliveries in a row on 2 Jan 2026, 10.04.05 WIB.",
    );
    expect(screen.getByText("Disabled after repeated failures")).toBeInTheDocument();
    await user.click(within(warning.closest("[role=alert]") as HTMLElement).getByRole("button", { name: "Enable" }));
    const dialog = await screen.findByRole("dialog", { name: "Enable this endpoint?" });
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));
    await waitFor(() => expect(api.to("POST", `${ITEM}/enable`)).toHaveLength(1));
    expect(await screen.findByText("Endpoint enabled")).toBeInTheDocument();
  });

  it("shows a manual disable as Disabled without the warning", async () => {
    routes({
      [LIST]: page([
        webhookEndpoint({ status: "disabled", disabled_reason: "manual", disabled_at: "2026-01-02T03:04:05Z" }),
      ]),
    });
    renderApp(PATH);
    expect(await screen.findByText("Disabled")).toBeInTheDocument();
    expect(screen.queryByText(/Disabled automatically/)).not.toBeInTheDocument();
  });

  it("is read-only for viewers", async () => {
    routes({ "GET /admin/me": me({ role: "viewer" }) });
    renderApp(PATH);
    expect(await screen.findByText("https://shop.example.com/hooks/keipay")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for/ })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Add endpoint" })).not.toBeInTheDocument();
  });

  it("renders cards at mobile width", async () => {
    setViewport("mobile");
    routes();
    renderApp(PATH);
    const cards = await screen.findByTestId("data-cards");
    expect(within(cards).getByText("https://shop.example.com/hooks/keipay")).toBeInTheDocument();
    expect(within(cards).getByRole("button", { name: /^Actions for/ })).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });
});

describe("Add endpoint sheet", () => {
  async function openSheet() {
    const rendered = renderApp(PATH);
    await rendered.user.click((await screen.findAllByRole("button", { name: "Add endpoint" }))[0]!);
    const sheet = await screen.findByRole("dialog", { name: "Add endpoint" });
    return { ...rendered, sheet };
  }

  it("builds the events picker from the catalog; * and payment.* disable what they cover", async () => {
    routes();
    const { user, sheet } = await openSheet();
    const all = await within(sheet).findByRole("checkbox", { name: "All events" });
    const payments = within(sheet).getByRole("checkbox", { name: "All payment events" });
    const paid = within(sheet).getByRole("checkbox", { name: "payment.paid" });
    expect(within(sheet).getByText("Every event, including types added later.")).toBeInTheDocument();
    expect(within(sheet).getAllByRole("checkbox")).toHaveLength(5);

    await user.click(paid);
    expect(paid).toBeChecked();
    await user.click(payments);
    expect(payments).toBeChecked();
    expect(paid).toBeDisabled();
    expect(within(sheet).getByRole("checkbox", { name: "payment.expired" })).toBeDisabled();

    await user.click(all);
    expect(all).toBeChecked();
    expect(payments).toBeDisabled();
    expect(payments).toBeChecked();
  });

  it("shows the catalog skeleton, then an inline error with retry", async () => {
    let calls = 0;
    routes({
      [LIST]: page([]),
      "GET /admin/event-types": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, eventCatalog());
      },
    });
    const { user, sheet } = await openSheet();
    expect(await within(sheet).findByText("Couldn't load the event types.")).toBeInTheDocument();
    await user.click(within(sheet).getByRole("button", { name: "Try again" }));
    expect(await within(sheet).findByRole("checkbox", { name: "payment.paid" })).toBeInTheDocument();
  });

  it("validates before sending", async () => {
    const api = routes();
    const { user, sheet } = await openSheet();
    await within(sheet).findByRole("checkbox", { name: "payment.paid" });
    await user.click(screen.getAllByRole("button", { name: "Add endpoint" }).at(-1)!);
    expect(await within(sheet).findByText("Enter the endpoint URL.")).toBeInTheDocument();
    expect(within(sheet).getByText("Choose at least one event.")).toBeInTheDocument();
    expect(api.to("POST", `/admin/apps/${APP_ID}/webhook-endpoints`)).toHaveLength(0);
  });

  it("sends the payload, then reveals the secret once", async () => {
    const created = webhookEndpoint({ headers: [{ name: "Authorization", value_last4: null }] });
    const api = routes({ [CREATE]: () => json(201, { endpoint: created, secret: WEBHOOK_SECRET }) });
    const { user, sheet } = await openSheet();
    await user.type(within(sheet).getByLabelText("Endpoint URL"), "https://shop.example.com/hooks/keipay");
    await user.type(within(sheet).getByLabelText("Description (optional)"), "Order service");
    await user.click(await within(sheet).findByRole("checkbox", { name: "payment.settled" }));
    await user.click(within(sheet).getByRole("checkbox", { name: "payment.paid" }));
    await user.click(within(sheet).getByRole("switch", { name: "Also send X-Kei-Signature" }));
    await user.click(within(sheet).getByRole("button", { name: "Add header" }));
    await user.type(within(sheet).getByLabelText("Header 1 name"), "Authorization");
    await user.type(within(sheet).getByLabelText("Header 1 value"), "Bearer abc");
    const attempts = within(sheet).getByLabelText("Max attempts");
    await user.clear(attempts);
    await user.type(attempts, "5");
    await user.click(screen.getAllByRole("button", { name: "Add endpoint" }).at(-1)!);

    const reveal = await screen.findByRole("dialog", { name: "Save the signing secret" });
    expect(within(reveal).getByText(WEBHOOK_SECRET)).toBeInTheDocument();
    expect(await api.to("POST", `/admin/apps/${APP_ID}/webhook-endpoints`)[0]!.json()).toEqual({
      url: "https://shop.example.com/hooks/keipay",
      description: "Order service",
      events: ["payment.settled", "payment.paid"],
      api_version: "v2",
      legacy_signature: true,
      headers: [{ name: "Authorization", value: "Bearer abc" }],
      retry_policy: { max_attempts: 5, backoff: "exponential", interval_seconds: 30 },
    });

    const done = within(reveal).getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();
    await user.keyboard("{Escape}");
    expect(screen.getByRole("dialog", { name: "Save the signing secret" })).toBeInTheDocument();
    await user.click(within(reveal).getByRole("checkbox", { name: "I have saved this" }));
    await user.click(done);
    await waitFor(() => expect(screen.queryByText(WEBHOOK_SECRET)).not.toBeInTheDocument());
  });

  it("maps server field errors onto the fields", async () => {
    routes({
      [CREATE]: () =>
        json(
          400,
          errorBody("invalid_request", "invalid request", {
            fields: [
              { field: "events.1", rule: "duplicate" },
              { field: "headers.0.name", rule: "reserved" },
              { field: "retry_policy.interval_seconds", rule: "max" },
            ],
          }),
        ),
    });
    const { user, sheet } = await openSheet();
    await user.type(within(sheet).getByLabelText("Endpoint URL"), "https://shop.example.com/hook");
    await user.click(await within(sheet).findByRole("checkbox", { name: "payment.paid" }));
    await user.click(within(sheet).getByRole("button", { name: "Add header" }));
    await user.type(within(sheet).getByLabelText("Header 1 name"), "X-Trace");
    await user.type(within(sheet).getByLabelText("Header 1 value"), "abc");
    await user.click(screen.getAllByRole("button", { name: "Add endpoint" }).at(-1)!);

    expect(await within(sheet).findByText("An event is chosen twice.")).toBeInTheDocument();
    expect(within(sheet).getByLabelText("Header 1 name")).toHaveAttribute("aria-invalid", "true");
    expect(within(sheet).getByText("KeiPay sets this header itself. Choose another name.")).toBeInTheDocument();
    expect(within(sheet).getByLabelText("Interval (seconds)")).toHaveAttribute("aria-invalid", "true");
    expect(within(sheet).getByText("Use a whole number from 10 to 3600.")).toBeInTheDocument();
    expect(within(sheet).queryByRole("alert")).not.toBeInTheDocument();
  });

  it("shows the 10-endpoint limit in an alert", async () => {
    routes({
      [CREATE]: () => json(400, errorBody("validation_failed", "an app can have at most 10 webhook endpoints")),
    });
    const { user, sheet } = await openSheet();
    await user.type(within(sheet).getByLabelText("Endpoint URL"), "https://shop.example.com/hook");
    await user.click(await within(sheet).findByRole("checkbox", { name: "All events" }));
    await user.click(screen.getAllByRole("button", { name: "Add endpoint" }).at(-1)!);
    expect(await within(sheet).findByRole("alert")).toHaveTextContent("An app can have at most 10 webhook endpoints.");
  });

  it("asks for https on a live app", async () => {
    routes({ [DETAIL]: { app: app({ environment: "live" }) } });
    const { user, sheet } = await openSheet();
    await user.type(within(sheet).getByLabelText("Endpoint URL"), "http://shop.example.com/hook");
    await user.click(await within(sheet).findByRole("checkbox", { name: "All events" }));
    await user.click(screen.getAllByRole("button", { name: "Add endpoint" }).at(-1)!);
    expect(await within(sheet).findByText("Live apps need an https URL.")).toBeInTheDocument();
  });
});

describe("Edit endpoint", () => {
  const stored = webhookEndpoint({ headers: [{ name: "Authorization", value_last4: "9xyz" }] });

  async function openEdit(api: ReturnType<typeof routes>) {
    const rendered = renderApp(PATH);
    await openMenu(rendered.user);
    await rendered.user.click(await screen.findByRole("menuitem", { name: "Edit" }));
    const sheet = await screen.findByRole("dialog", { name: "Edit endpoint" });
    return { ...rendered, sheet, api };
  }

  it("sends only the changed fields and leaves headers out", async () => {
    const api = routes({ [LIST]: page([stored]), [`PATCH ${ITEM}`]: { endpoint: stored } });
    const { user, sheet } = await openEdit(api);
    expect(within(sheet).getByText("Authorization")).toBeInTheDocument();
    expect(within(sheet).getByText("…9xyz")).toBeInTheDocument();
    await user.click(await within(sheet).findByRole("checkbox", { name: "payment.expired" }));
    await user.click(within(sheet).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.to("PATCH", ITEM)).toHaveLength(1));
    expect(await api.to("PATCH", ITEM)[0]!.json()).toEqual({
      events: ["payment.paid", "payment.settled", "payment.expired"],
    });
    expect(await screen.findByText("Endpoint updated")).toBeInTheDocument();
  });

  it("sends the whole header set after Replace headers", async () => {
    const api = routes({ [LIST]: page([stored]), [`PATCH ${ITEM}`]: { endpoint: stored } });
    const { user, sheet } = await openEdit(api);
    await user.click(within(sheet).getByRole("button", { name: "Replace headers" }));
    expect(within(sheet).getByLabelText("Header 1 name")).toHaveValue("Authorization");
    expect(within(sheet).getByLabelText("Header 1 value")).toHaveValue("");
    await user.type(within(sheet).getByLabelText("Header 1 value"), "Bearer new");
    await user.click(within(sheet).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.to("PATCH", ITEM)).toHaveLength(1));
    expect(await api.to("PATCH", ITEM)[0]!.json()).toEqual({
      headers: [{ name: "Authorization", value: "Bearer new" }],
    });
  });

  it("can go back to the stored headers, and closes without a request when nothing changed", async () => {
    const api = routes({ [LIST]: page([stored]) });
    const { user, sheet } = await openEdit(api);
    await user.click(within(sheet).getByRole("button", { name: "Replace headers" }));
    await user.click(within(sheet).getByRole("button", { name: "Keep current headers" }));
    expect(within(sheet).getByText("…9xyz")).toBeInTheDocument();
    await user.click(within(sheet).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Edit endpoint" })).not.toBeInTheDocument());
    expect(api.to("PATCH", ITEM)).toHaveLength(0);
  });
});

describe("Row actions", () => {
  it("rotates after the URL host is typed, with the chosen overlap, and reveals the new secret", async () => {
    const api = routes({
      [`POST ${ITEM}/rotate-secret`]: () =>
        json(200, {
          endpoint: webhookEndpoint({ previous_secret_expires_at: "2026-01-02T00:00:00Z" }),
          secret: WEBHOOK_SECRET,
        }),
    });
    const { user } = renderApp(PATH);
    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Rotate secret" }));
    const dialog = await screen.findByRole("dialog", { name: "Rotate the signing secret?" });
    const confirm = within(dialog).getByRole("button", { name: "Rotate secret" });
    expect(within(dialog).getByLabelText("Overlap (hours)")).toHaveValue("24");
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByLabelText("Type shop.example.com to confirm"), "shop.example.com");
    expect(confirm).toBeEnabled();
    const overlap = within(dialog).getByLabelText("Overlap (hours)");
    await user.clear(overlap);
    await user.type(overlap, "169");
    expect(confirm).toBeDisabled();
    expect(within(dialog).getByText("Use a whole number from 0 to 168.")).toBeInTheDocument();
    await user.clear(overlap);
    await user.type(overlap, "0");
    await user.click(confirm);

    const reveal = await screen.findByRole("dialog", { name: "Save the signing secret" });
    expect(within(reveal).getByText(WEBHOOK_SECRET)).toBeInTheDocument();
    expect(within(reveal).getByText(/Until 2 Jan 2026, 07.00.00 WIB requests are also signed with the old one/)).toBeInTheDocument();
    expect(await api.to("POST", `${ITEM}/rotate-secret`)[0]!.json()).toEqual({ overlap_hours: 0 });
    expect(within(reveal).getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("deletes only after the URL host is typed", async () => {
    const api = routes({ [`DELETE ${ITEM}`]: () => new Response(null, { status: 204 }) });
    const { user } = renderApp(PATH);
    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Delete" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete this endpoint?" });
    const confirm = within(dialog).getByRole("button", { name: "Delete" });
    await user.type(within(dialog).getByLabelText("Type shop.example.com to confirm"), "shop.example");
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByLabelText("Type shop.example.com to confirm"), ".com");
    await user.click(confirm);
    await waitFor(() => expect(api.to("DELETE", ITEM)).toHaveLength(1));
    expect(await screen.findByText("Endpoint deleted")).toBeInTheDocument();
  });

  it("disables with a plain confirm", async () => {
    const api = routes({ [`POST ${ITEM}/disable`]: { endpoint: webhookEndpoint({ status: "disabled" }) } });
    const { user } = renderApp(PATH);
    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog", { name: "Disable this endpoint?" });
    await user.click(within(dialog).getByRole("button", { name: "Disable" }));
    await waitFor(() => expect(api.to("POST", `${ITEM}/disable`)).toHaveLength(1));
  });

  it("sends a test event and links to the delivery", async () => {
    const api = routes({
      [`POST ${ITEM}/test`]: () => json(202, { delivery: delivery({ status: "PENDING", event: { id: "evt_x", type: "payment.settled", test: true } }) }),
      "GET /admin/apps": page([app()]),
      "GET /admin/webhook-deliveries": page([]),
      [`GET /admin/webhook-deliveries/${DLV_ID}`]: () => new Promise<Response>(() => {}),
    });
    const { user, router } = renderApp(PATH);
    await openMenu(user);
    await user.click(await screen.findByRole("menuitem", { name: "Send test event" }));
    const dialog = await screen.findByRole("dialog", { name: "Send a test event" });
    await user.click(await within(dialog).findByRole("combobox", { name: "Event type" }));
    await user.click(await screen.findByRole("option", { name: /payment\.settled/ }));
    await user.click(within(dialog).getByRole("button", { name: "Send test event" }));
    await waitFor(() => expect(api.to("POST", `${ITEM}/test`)).toHaveLength(1));
    expect(await api.to("POST", `${ITEM}/test`)[0]!.json()).toEqual({ event_type: "payment.settled" });
    expect(await screen.findByText("Test event queued")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "View delivery" }));
    await waitFor(() => expect(router.state.location.search).toBe(`?tab=outbound&id=${DLV_ID}`));
    expect(router.state.location.pathname).toBe("/webhooks");
  });

  it("links to the endpoint's deliveries", async () => {
    routes();
    const { user } = renderApp(PATH);
    await openMenu(user);
    expect(await screen.findByRole("menuitem", { name: "View deliveries" })).toHaveAttribute(
      "href",
      `/webhooks?tab=outbound&endpoint_id=${WHE_ID}`,
    );
  });
});
