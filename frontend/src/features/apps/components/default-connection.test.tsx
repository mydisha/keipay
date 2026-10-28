import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, CON_ID, connectionListItem, connectionSummary, me, page } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

// T-55: the default connection on the app Overview tab.

const DETAIL = `GET /admin/apps/${APP_ID}`;
const PATCH = `PATCH /admin/apps/${APP_ID}`;
const OTHER_ID = "con_01JBKQ7Z8M4X2Y6V3N9T5R1W0K";

beforeEach(() => {
  setViewport("desktop");
});

async function bodyOf(request: Request | undefined): Promise<unknown> {
  return request ? JSON.parse(await request.clone().text()) : undefined;
}

function section() {
  return screen.findByRole("region", { name: "Default connection" });
}

const options = page([
  connectionListItem(),
  connectionListItem({ id: OTHER_ID, name: "xendit-backup" }),
]);

describe("Default connection (T-55)", () => {
  it("offers active connections of the app's environment and sets one", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      "GET /admin/connections": options,
      [PATCH]: { app: app({ default_connection: connectionSummary() }) },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    const region = await section();
    expect(await within(region).findByText("Payments for this app fail until a default connection is set.")).toBeInTheDocument();

    const query = new URL(api.to("GET", "/admin/connections")[0]!.url).searchParams;
    expect(Object.fromEntries(query)).toEqual({ environment: "test", status: "active", limit: "200" });

    const select = within(region).getByRole("combobox", { name: "Default connection" });
    expect(select).toHaveTextContent("None");
    await user.click(select);
    expect((await screen.findAllByRole("option")).map((o) => o.textContent)).toEqual([
      "None",
      "xendit-test",
      "xendit-backup",
    ]);
    await user.click(screen.getByRole("option", { name: "xendit-test" }));
    await waitFor(() => expect(api.to("PATCH", `/admin/apps/${APP_ID}`)).toHaveLength(1));
    expect(await bodyOf(api.to("PATCH", `/admin/apps/${APP_ID}`)[0])).toEqual({ default_connection_id: CON_ID });
    expect(await screen.findByText("Default connection of Demo App updated")).toBeInTheDocument();
    expect(within(region).getByRole("combobox", { name: "Default connection" })).toHaveTextContent("xendit-test");
    expect(within(region).queryByText(/Payments for this app fail/)).not.toBeInTheDocument();
  });

  it("sends null for None", async () => {
    const api = mockApi({
      [DETAIL]: { app: app({ default_connection: connectionSummary() }) },
      "GET /admin/connections": options,
      [PATCH]: { app: app() },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    const region = await section();
    const select = await within(region).findByRole("combobox", { name: "Default connection" });
    expect(select).toHaveTextContent("xendit-test");
    await user.click(select);
    await user.click(await screen.findByRole("option", { name: "None" }));
    await waitFor(() => expect(api.to("PATCH", `/admin/apps/${APP_ID}`)).toHaveLength(1));
    expect(await bodyOf(api.to("PATCH", `/admin/apps/${APP_ID}`)[0])).toEqual({ default_connection_id: null });
    expect(await within(region).findByText(/Payments for this app fail/)).toBeInTheDocument();
  });

  it.each([
    ["environment", "Pick a connection with the same environment as the app."],
    ["status", "That connection is disabled. Pick an active one."],
    ["exists", "That connection no longer exists. Pick another one."],
  ])("shows the %s field error", async (rule, message) => {
    mockApi({
      [DETAIL]: { app: app() },
      "GET /admin/connections": options,
      [PATCH]: () =>
        json(400, errorBody("invalid_request", "invalid request", { fields: [{ field: "default_connection_id", rule }] })),
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    const region = await section();
    await user.click(await within(region).findByRole("combobox", { name: "Default connection" }));
    await user.click(await screen.findByRole("option", { name: "xendit-backup" }));
    expect(await within(region).findByText(message)).toBeInTheDocument();
    expect(within(region).getByRole("combobox", { name: "Default connection" })).toHaveAttribute("aria-invalid", "true");
    expect(within(region).getByRole("combobox", { name: "Default connection" })).toHaveAccessibleDescription(message);
  });

  it("links to the wizard for the app's environment when there are no options", async () => {
    mockApi({ [DETAIL]: { app: app({ environment: "live" }) }, "GET /admin/connections": page([]) });
    renderApp(`/apps/${APP_ID}`);
    const region = await section();
    expect(await within(region).findByText("No active live connections.")).toBeInTheDocument();
    expect(within(region).getByRole("link", { name: "Add connection" })).toHaveAttribute(
      "href",
      "/connections/new?environment=live",
    );
    expect(within(region).queryByRole("combobox")).not.toBeInTheDocument();
  });

  it("shows a skeleton while the options load and a retry when they fail", async () => {
    let calls = 0;
    mockApi({
      [DETAIL]: { app: app() },
      "GET /admin/connections": () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, options);
      },
    });
    const { user } = renderApp(`/apps/${APP_ID}`);
    const region = await section();
    expect(within(region).getByTestId("default-connection-skeleton")).toBeInTheDocument();
    expect(await within(region).findByText("Couldn't load the connections.")).toBeInTheDocument();
    await user.click(within(region).getByRole("button", { name: "Try again" }));
    expect(await within(region).findByRole("combobox", { name: "Default connection" })).toBeInTheDocument();
  });

  it("is a link for viewers, or Not set", async () => {
    const api = mockApi({
      "GET /admin/me": me({ role: "viewer" }),
      [DETAIL]: { app: app({ default_connection: connectionSummary() }) },
    });
    renderApp(`/apps/${APP_ID}`);
    const region = await section();
    expect(within(region).getByRole("link", { name: "xendit-test" })).toHaveAttribute("href", `/connections/${CON_ID}`);
    expect(within(region).queryByRole("combobox")).not.toBeInTheDocument();
    expect(api.to("GET", "/admin/connections")).toHaveLength(0);
  });

  it("says Not set to viewers with the warning", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), [DETAIL]: { app: app() } });
    renderApp(`/apps/${APP_ID}`);
    const region = await section();
    expect(within(region).getByText("Not set")).toBeInTheDocument();
    expect(within(region).getByText("Payments for this app fail until a default connection is set.")).toBeInTheDocument();
  });
});
