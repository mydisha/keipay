import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, me } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// T-43 (header, states, disable/enable, edit) and T-45 (placeholder tabs).

const DETAIL = `GET /admin/apps/${APP_ID}`;

beforeEach(() => {
  setViewport("desktop");
});

describe("App detail (T-43)", () => {
  it("shows the header facts and the overview", async () => {
    mockApi({
      [DETAIL]: {
        app: app({
          metadata: { team: "growth" },
          redirect_success_url: "https://demo.example.com/thanks",
        }),
      },
    });
    renderApp(`/apps/${APP_ID}`);
    expect(await screen.findByRole("heading", { level: 1, name: "Demo App" })).toBeInTheDocument();
    expect(screen.getAllByText("Test")[0]).toHaveAttribute("data-environment", "test");
    expect(screen.getAllByText("Active").length).toBeGreaterThan(0);
    expect(screen.getAllByText(APP_ID).length).toBeGreaterThan(0);
    expect(screen.getByRole("button", { name: `Copy App ID ${APP_ID}` })).toBeInTheDocument();

    const tabs = screen.getByRole("navigation", { name: "App sections" });
    expect(within(tabs).getAllByRole("link").map((l) => l.textContent)).toEqual([
      "Overview",
      "API keys",
      "Webhooks",
      "Routing",
    ]);
    expect(within(tabs).getByRole("link", { name: "Overview" })).toHaveAttribute("aria-current", "page");

    const details = await screen.findByRole("region", { name: "Details" });
    expect(within(details).getByText("https://demo.example.com/webhooks/kei")).toBeInTheDocument();
    expect(within(details).getByText("https://demo.example.com/thanks")).toBeInTheDocument();
    expect(within(details).getAllByText("Not set")).toHaveLength(1);
    expect(screen.getByRole("region", { name: "Metadata" })).toHaveTextContent('"team": "growth"');
  });

  it("says No metadata and Not set for an empty app", async () => {
    mockApi({ [DETAIL]: { app: app({ webhook_url: "" }) } });
    renderApp(`/apps/${APP_ID}`);
    const details = await screen.findByRole("region", { name: "Details" });
    expect(within(details).getAllByText("Not set")).toHaveLength(3);
    expect(screen.getByText("No metadata")).toBeInTheDocument();
  });

  it("shows a header skeleton while loading", async () => {
    mockApi({ [DETAIL]: () => new Promise<Response>(() => {}) });
    renderApp(`/apps/${APP_ID}`);
    expect(await screen.findByTestId("app-header-skeleton")).toBeInTheDocument();
  });

  it.each([
    ["an unknown id (404)", () => json(404, errorBody("not_found", "not found"))],
    [
      "a malformed id (400)",
      () => json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "id", rule: "public_id" }] })),
    ],
  ])("shows App not found for %s", async (_, handler) => {
    mockApi({ "GET /admin/apps/app_nope": handler });
    renderApp("/apps/app_nope");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("App not found");
    expect(within(empty).getByRole("link", { name: "Back to apps" })).toHaveAttribute("href", "/apps");
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      [DETAIL]: () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, { app: app() });
      },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: "Demo App" })).toBeInTheDocument();
  });

  it("shows ForbiddenState for 403 forbidden", async () => {
    mockApi({ [DETAIL]: () => json(403, errorBody("forbidden", "forbidden")) });
    renderApp(`/apps/${APP_ID}`);
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByTestId("error-state")).not.toBeInTheDocument();
  });

  it("disables only after the slug is typed exactly", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`POST /admin/apps/${APP_ID}/disable`]: { app: app({ status: "disabled" }) },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    await user.click(await screen.findByRole("button", { name: "Disable" }));
    const confirm = await screen.findByRole("dialog", { name: "Disable Demo App?" });
    expect(confirm).toHaveTextContent("tenant_inactive");
    const button = within(confirm).getByRole("button", { name: "Disable" });
    const input = within(confirm).getByRole("textbox", { name: "Type demo to confirm" });
    await user.type(input, "DEMO");
    expect(button).toBeDisabled();
    await user.clear(input);
    await user.type(input, "demo");
    await user.click(button);
    await waitFor(() => expect(api.to("POST", `/admin/apps/${APP_ID}/disable`)).toHaveLength(1));
    expect(await screen.findByText("Demo App disabled")).toBeInTheDocument();
    // The header shows the new status and offers Enable.
    expect(await screen.findByRole("button", { name: "Enable" })).toBeInTheDocument();
    expect(screen.getAllByText("Disabled").length).toBeGreaterThan(0);
  });

  it("enables with a plain confirmation", async () => {
    const api = mockApi({
      [DETAIL]: { app: app({ status: "disabled" }) },
      [`POST /admin/apps/${APP_ID}/enable`]: { app: app() },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    await user.click(await screen.findByRole("button", { name: "Enable" }));
    const confirm = await screen.findByRole("dialog", { name: "Enable Demo App?" });
    expect(within(confirm).queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(within(confirm).getByRole("button", { name: "Enable" }));
    await waitFor(() => expect(api.to("POST", `/admin/apps/${APP_ID}/enable`)).toHaveLength(1));
    expect(await screen.findByRole("button", { name: "Disable" })).toBeInTheDocument();
  });

  it("edit sends only the changed fields", async () => {
    const api = mockApi({
      [DETAIL]: { app: app({ metadata: { team: "growth" } }) },
      [`PATCH /admin/apps/${APP_ID}`]: { app: app({ name: "Demo Renamed", metadata: { team: "growth", tier: 2 } }) },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Demo App" });
    expect(within(dialog).getByLabelText("Metadata")).toHaveValue('{\n  "team": "growth"\n}');
    await user.clear(within(dialog).getByLabelText("Name"));
    await user.type(within(dialog).getByLabelText("Name"), "Demo Renamed");
    await user.clear(within(dialog).getByLabelText("Metadata"));
    await user.type(within(dialog).getByLabelText("Metadata"), '{{"team": "growth", "tier": 2}');
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await api.to("PATCH", `/admin/apps/${APP_ID}`)[0]!.json()).toEqual({
      name: "Demo Renamed",
      metadata: { team: "growth", tier: 2 },
    });
    expect(await screen.findByRole("heading", { level: 1, name: "Demo Renamed" })).toBeInTheDocument();
  });

  it("edit without changes sends nothing", async () => {
    const api = mockApi({ [DETAIL]: { app: app({ metadata: { team: "growth" } }) } });
    const { user } = renderApp(`/apps/${APP_ID}`);
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Demo App" });
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(api.to("PATCH", `/admin/apps/${APP_ID}`)).toHaveLength(0);
  });

  it("edit puts a server metadata error on the field", async () => {
    mockApi({
      [DETAIL]: { app: app() },
      [`PATCH /admin/apps/${APP_ID}`]: () =>
        json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "metadata", rule: "max_bytes" }] })),
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    await user.click(await screen.findByRole("button", { name: "Edit" }));
    const dialog = await screen.findByRole("dialog", { name: "Edit Demo App" });
    await user.type(within(dialog).getByLabelText("Metadata"), '{{"a": 1}');
    await user.click(within(dialog).getByRole("button", { name: "Save changes" }));
    expect(await within(dialog).findByText("Metadata is larger than 8192 bytes.")).toBeInTheDocument();
  });

  it("viewers see no Edit, Disable or Enable", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), [DETAIL]: { app: app() } });
    renderApp(`/apps/${APP_ID}`);
    await screen.findByRole("heading", { level: 1, name: "Demo App" });
    expect(screen.queryByRole("button", { name: "Edit" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Disable" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Enable" })).not.toBeInTheDocument();
  });
});

describe("Webhooks and Routing tabs (T-45)", () => {
  it.each([
    ["webhooks", "Webhook endpoints are coming soon"],
    ["routing", "Routing is coming soon"],
  ])("/%s shows its empty state without API calls beyond the app", async (tab, title) => {
    const api = mockApi({ [DETAIL]: { app: app() } });
    renderApp(`/apps/${APP_ID}/${tab}`);
    expect(await screen.findByText(title)).toBeInTheDocument();
    const adminCalls = api.requests
      .map((r) => `${r.method} ${new URL(r.url).pathname}`)
      .filter((call) => call !== "GET /admin/me" && call !== "GET /admin/setup");
    expect(adminCalls).toEqual([DETAIL]);
  });

  it("links the webhooks tab to the webhook URL in Overview", async () => {
    mockApi({ [DETAIL]: { app: app() } });
    renderApp(`/apps/${APP_ID}/webhooks`);
    expect(await screen.findByRole("link", { name: "View webhook URL" })).toHaveAttribute(
      "href",
      `/apps/${APP_ID}#webhook-url`,
    );
  });
});
