import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { APP_ID, appSummary, CON_ID, connection, me, provider } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const DETAIL = `GET /admin/connections/${CON_ID}`;
const PATH = `/connections/${CON_ID}`;
const providers = { "GET /admin/providers": { data: [provider()] } };

beforeEach(() => {
  setViewport("desktop");
});

async function bodyOf(request: Request | undefined): Promise<unknown> {
  return request ? JSON.parse(await request.clone().text()) : undefined;
}

function region(name: string) {
  return screen.getByRole("region", { name });
}

describe("Connection detail", () => {
  it("shows the header, facts, callback URL and credential hints without values", async () => {
    mockApi({
      ...providers,
      [DETAIL]: {
        connection: connection({
          legacy_webhook_alias: true,
          last_test_status: "failed",
          last_tested_at: "2026-01-01T00:05:00Z",
          last_test_error: "The provider rejected the credentials (HTTP 401).",
        }),
      },
    });
    renderApp(PATH);
    expect(await screen.findByRole("heading", { level: 1, name: "xendit-test" })).toBeInTheDocument();
    expect(screen.getAllByText("Xendit").length).toBeGreaterThan(0);
    expect(screen.getAllByText("Test")[0]).toHaveAttribute("data-environment", "test");
    expect(screen.getByText("Active")).toBeInTheDocument();
    expect(screen.getByText("Legacy URL")).toBeInTheDocument();

    const overview = region("Overview");
    expect(within(overview).getByText(CON_ID)).toBeInTheDocument();
    expect(within(overview).getByText("Failed")).toBeInTheDocument();
    expect(within(overview).getByText("The provider rejected the credentials (HTTP 401).")).toBeInTheDocument();

    const callback = region("Callback URL");
    expect(within(callback).getByTestId("callback-url")).toHaveTextContent(`/webhooks/xendit/${CON_ID}`);
    expect(within(callback).getByText("Also receives callbacks at /webhooks/xendit (legacy).")).toBeInTheDocument();
    expect(within(callback).getByText("Open Settings → Webhooks.")).toBeInTheDocument();

    const credentials = region("Credentials");
    expect(within(credentials).getByText("Secret API key").nextSibling).toHaveTextContent("•••• a1b2");
    expect(within(credentials).getByText("Webhook verification token").nextSibling).toHaveTextContent("Stored");
    expect(credentials.querySelector("input")).toBeNull();
  });

  it("replaces only the filled credentials and then shows Not tested", async () => {
    const api = mockApi({
      ...providers,
      [DETAIL]: { connection: connection({ last_test_status: "ok", last_tested_at: "2026-01-01T00:05:00Z" }) },
      [`PATCH /admin/connections/${CON_ID}/credentials`]: {
        connection: connection({ credentials: [{ key: "secret_key", last4: "9z9z" }, { key: "callback_token", last4: null }] }),
      },
    });
    const { user } = renderApp(PATH);
    await screen.findByRole("heading", { level: 1, name: "xendit-test" });
    expect(within(region("Overview")).getByText("Passed")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Replace credentials" }));
    const dialog = await screen.findByRole("dialog", { name: "Replace credentials" });
    const secret = within(dialog).getByLabelText(/Secret API key/, { selector: "input" });
    expect(secret).toHaveValue("");
    expect(secret).toHaveAttribute("placeholder", "•••• a1b2");

    await user.click(within(dialog).getByRole("button", { name: "Replace credentials" }));
    expect(await within(dialog).findByText("Enter at least one new value.")).toBeInTheDocument();
    expect(api.to("PATCH", `/admin/connections/${CON_ID}/credentials`)).toHaveLength(0);

    await user.type(secret, "xnd_development_new9z9z");
    await user.click(within(dialog).getByRole("button", { name: "Replace credentials" }));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(await bodyOf(api.to("PATCH", `/admin/connections/${CON_ID}/credentials`)[0])).toEqual({
      credentials: { secret_key: "xnd_development_new9z9z" },
    });
    expect(within(region("Overview")).getByText("Not tested")).toBeInTheDocument();
    expect(within(region("Credentials")).getByText("•••• 9z9z")).toBeInTheDocument();
    expect(screen.queryByText("xnd_development_new9z9z")).not.toBeInTheDocument();
  });

  it("saves only the changed details", async () => {
    const api = mockApi({
      ...providers,
      [DETAIL]: { connection: connection({ settings: { invoice_duration: 3600 } }) },
      [`PATCH /admin/connections/${CON_ID}`]: {
        connection: connection({ name: "xendit-main", settings: { invoice_duration: 3600 } }),
      },
    });
    const { user } = renderApp(PATH);
    const details = await screen.findByRole("region", { name: "Details" });
    expect(within(details).getByLabelText("Invoice lifetime (seconds)")).toHaveValue("3600");

    await user.click(within(details).getByRole("button", { name: "Save changes" }));
    expect(await screen.findByText("Nothing to save.")).toBeInTheDocument();
    expect(api.to("PATCH", `/admin/connections/${CON_ID}`)).toHaveLength(0);

    const name = within(details).getByLabelText("Name");
    await user.clear(name);
    await user.type(name, "Xendit-Main");
    await user.click(within(details).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.to("PATCH", `/admin/connections/${CON_ID}`)).toHaveLength(1));
    expect(await bodyOf(api.to("PATCH", `/admin/connections/${CON_ID}`)[0])).toEqual({ name: "xendit-main" });

    const lifetime = within(details).getByLabelText("Invoice lifetime (seconds)");
    await user.clear(lifetime);
    await user.click(within(details).getByRole("button", { name: "Save changes" }));
    await waitFor(() => expect(api.to("PATCH", `/admin/connections/${CON_ID}`)).toHaveLength(2));
    expect(await bodyOf(api.to("PATCH", `/admin/connections/${CON_ID}`)[1])).toEqual({ settings: {} });
  });

  it("puts a duplicate name error on the name field", async () => {
    mockApi({
      ...providers,
      [DETAIL]: { connection: connection() },
      [`PATCH /admin/connections/${CON_ID}`]: () => json(409, errorBody("conflict", "name taken")),
    });
    const { user } = renderApp(PATH);
    const details = await screen.findByRole("region", { name: "Details" });
    await user.type(within(details).getByLabelText("Name"), "-2");
    await user.click(within(details).getByRole("button", { name: "Save changes" }));
    expect(await within(details).findByText("A connection with this name already exists.")).toBeInTheDocument();
  });

  it("says when the provider has no settings", async () => {
    mockApi({
      "GET /admin/providers": { data: [provider({ settings_schema: { fields: [] } })] },
      [DETAIL]: { connection: connection() },
    });
    renderApp(PATH);
    expect(await screen.findByText("This provider has no settings.")).toBeInTheDocument();
  });

  it("saves channels", async () => {
    const api = mockApi({
      ...providers,
      [DETAIL]: { connection: connection() },
      [`PUT /admin/connections/${CON_ID}/channels`]: { connection: connection() },
    });
    const { user } = renderApp(PATH);
    const channels = await screen.findByRole("region", { name: "Channels" });
    await user.click(within(channels).getByRole("checkbox", { name: "QRIS" }));
    await user.click(within(channels).getByRole("checkbox", { name: "BCA Virtual Account" }));
    await user.click(within(channels).getByRole("checkbox", { name: "OVO" }));
    await user.click(within(channels).getByRole("button", { name: "Save channels" }));
    expect(await within(channels).findByText("Keep at least one channel on.")).toBeInTheDocument();
    await user.click(within(channels).getByRole("checkbox", { name: "OVO" }));
    await user.click(within(channels).getByRole("button", { name: "Save channels" }));
    expect(await screen.findByText("Channels saved")).toBeInTheDocument();
    expect(await bodyOf(api.to("PUT", `/admin/connections/${CON_ID}/channels`)[0])).toEqual({
      channels: [
        { channel: "QRIS", enabled: false },
        { channel: "VA_BCA", enabled: false },
        { channel: "EWALLET_OVO", enabled: true },
      ],
    });
  });

  it("tests the connection and shows the result", async () => {
    mockApi({
      ...providers,
      [DETAIL]: { connection: connection() },
      [`POST /admin/connections/${CON_ID}/test`]: {
        result: "failed",
        message: "Could not reach the provider. Try again later.",
        tested_at: "2026-01-01T00:05:00Z",
        connection: connection({
          last_test_status: "failed",
          last_tested_at: "2026-01-01T00:05:00Z",
          last_test_error: "Could not reach the provider. Try again later.",
        }),
      },
    });
    const { user } = renderApp(PATH);
    await user.click(await screen.findByRole("button", { name: "Test connection" }));
    expect(await screen.findByText("Test failed. Could not reach the provider. Try again later.")).toBeInTheDocument();
    expect(within(region("Overview")).getByText("Failed")).toBeInTheDocument();
  });

  it("disables only after the name is typed", async () => {
    const api = mockApi({
      ...providers,
      [DETAIL]: { connection: connection() },
      [`POST /admin/connections/${CON_ID}/disable`]: { connection: connection({ status: "disabled" }) },
    });
    const { user } = renderApp(PATH);
    await user.click(await screen.findByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog", { name: "Disable xendit-test?" });
    const confirm = within(dialog).getByRole("button", { name: "Disable" });
    expect(confirm).toBeDisabled();
    await user.type(within(dialog).getByRole("textbox"), "xendit-test");
    await user.click(confirm);
    expect(await screen.findByText("xendit-test disabled")).toBeInTheDocument();
    expect(api.to("POST", `/admin/connections/${CON_ID}/disable`)).toHaveLength(1);
    expect(screen.getByText("Disabled")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Enable" })).toBeInTheDocument();
  });

  it("lists the apps that use it when disabling is refused", async () => {
    mockApi({
      ...providers,
      [DETAIL]: { connection: connection() },
      [`POST /admin/connections/${CON_ID}/disable`]: () =>
        json(
          409,
          errorBody("connection_in_use", "connection is in use", {
            apps: [appSummary(), appSummary({ id: "app_01JBKQ7Z8M4X2Y6V3N9T5R1W0F", name: "Globex", slug: "globex" })],
            app_count: 12,
          }),
        ),
    });
    const { user } = renderApp(PATH);
    await user.click(await screen.findByRole("button", { name: "Disable" }));
    const confirmDialog = await screen.findByRole("dialog", { name: "Disable xendit-test?" });
    await user.type(within(confirmDialog).getByRole("textbox"), "xendit-test");
    await user.click(within(confirmDialog).getByRole("button", { name: "Disable" }));
    const dialog = await screen.findByRole("dialog", { name: "Connection in use" });
    expect(dialog).toHaveTextContent("12 apps use this connection as the default.");
    expect(within(dialog).getByRole("link", { name: "Demo App" })).toHaveAttribute("href", `/apps/${APP_ID}`);
    expect(within(dialog).getByRole("link", { name: "Globex" })).toBeInTheDocument();
    expect(within(dialog).getByText("And 10 more.")).toBeInTheDocument();
    expect(screen.getByText("Active")).toBeInTheDocument();
  });

  it("enables a disabled connection with a plain confirm", async () => {
    const api = mockApi({
      ...providers,
      [DETAIL]: { connection: connection({ status: "disabled" }) },
      [`POST /admin/connections/${CON_ID}/enable`]: { connection: connection() },
    });
    const { user } = renderApp(PATH);
    await user.click(await screen.findByRole("button", { name: "Enable" }));
    const dialog = await screen.findByRole("dialog", { name: "Enable xendit-test?" });
    expect(within(dialog).queryByRole("textbox")).not.toBeInTheDocument();
    await user.click(within(dialog).getByRole("button", { name: "Enable" }));
    expect(await screen.findByText("xendit-test enabled")).toBeInTheDocument();
    expect(api.to("POST", `/admin/connections/${CON_ID}/enable`)).toHaveLength(1);
  });

  it("is read-only for viewers", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), ...providers, [DETAIL]: { connection: connection() } });
    renderApp(PATH);
    await screen.findByRole("heading", { level: 1, name: "xendit-test" });
    for (const name of ["Test connection", "Disable", "Replace credentials", "Save changes", "Save channels"]) {
      expect(screen.queryByRole("button", { name })).not.toBeInTheDocument();
    }
    expect(within(region("Details")).getByLabelText("Name")).toBeDisabled();
    expect(within(region("Channels")).getByRole("checkbox", { name: "QRIS" })).toBeDisabled();
    expect(within(region("Credentials")).getByText("•••• a1b2")).toBeInTheDocument();
  });

  it.each([
    ["an unknown id (404)", () => json(404, errorBody("not_found", "not found"))],
    [
      "a malformed id (400)",
      () => json(400, errorBody("invalid_request", "invalid", { fields: [{ field: "id", rule: "public_id" }] })),
    ],
  ])("shows Connection not found for %s", async (_, handler) => {
    mockApi({ ...providers, "GET /admin/connections/con_nope": handler });
    renderApp("/connections/con_nope");
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("Connection not found");
    expect(within(empty).getByRole("link", { name: "Back to connections" })).toHaveAttribute("href", "/connections");
  });

  it("shows a skeleton while loading", async () => {
    mockApi({ ...providers, [DETAIL]: () => new Promise<Response>(() => {}) });
    renderApp(PATH);
    expect(await screen.findByTestId("connection-header-skeleton")).toBeInTheDocument();
  });

  it("shows an error with retry", async () => {
    let calls = 0;
    mockApi({
      ...providers,
      [DETAIL]: () => {
        calls += 1;
        return calls === 1
          ? json(500, errorBody("internal_error", "internal server error"))
          : json(200, { connection: connection() });
      },
    });
    const { user } = renderApp(PATH);
    const error = await screen.findByTestId("error-state");
    await user.click(within(error).getByRole("button", { name: "Try again" }));
    expect(await screen.findByRole("heading", { level: 1, name: "xendit-test" })).toBeInTheDocument();
  });

  it("shows ForbiddenState on 403", async () => {
    mockApi({ ...providers, [DETAIL]: () => json(403, errorBody("forbidden", "forbidden")) });
    renderApp(PATH);
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
  });
});
