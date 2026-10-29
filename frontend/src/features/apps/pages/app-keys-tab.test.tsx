import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { apiKey, app, APP_ID, me, page } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const DETAIL = `GET /admin/apps/${APP_ID}`;
const KEYS = `/admin/apps/${APP_ID}/keys`;
const KEY_ID = apiKey().id;

const used = apiKey({
  id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0G",
  label: "Production server",
  last4: "c3d4",
  last_used_at: "2026-01-02T00:00:00Z",
  expires_at: "2027-01-01T00:00:00Z",
});
const expired = apiKey({
  id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0H",
  label: "Old CI",
  last4: "e5f6",
  status: "expired",
  expires_at: "2026-01-01T00:00:00Z",
});
const revoked = apiKey({
  id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0J",
  label: "Leaked",
  last4: "g7h8",
  status: "revoked",
  revoked_at: "2026-01-01T00:00:00Z",
});
const legacy = apiKey({ id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0K", label: "Migrated key", prefix: "kp_", last4: "0001" });

const rotated = {
  key: apiKey({ id: "key_01JBKQ7Z8M4X2Y6V3N9T5R1W0M", last4: "z9y8" }),
  api_key: "kp_test_ffffffffffffffffffffffffffffz9y8",
  api_secret: "kps_test_rotated-secret",
};

beforeEach(() => {
  setViewport("desktop");
});

function row(text: string) {
  return screen.getByText(text, { selector: "td *" }).closest("tr")!;
}

describe("API keys tab", () => {
  it("lists keys as prefix…last4 with status text, Never and expiry", async () => {
    mockApi({ [DETAIL]: { app: app() }, [`GET ${KEYS}`]: page([apiKey(), used, expired, revoked, legacy]) });
    renderApp(`/apps/${APP_ID}/keys`);
    await screen.findByText("kp_test_…a1b2");
    const table = screen.getByRole("table", { name: "API keys" });
    expect(within(table).getByText("kp_…0001")).toBeInTheDocument();

    const fresh = row("kp_test_…a1b2");
    expect(within(fresh).getByText("Active")).toBeInTheDocument();
    expect(within(fresh).getAllByText("Never")).toHaveLength(2);
    expect(within(row("kp_test_…c3d4")).queryByText("Never")).not.toBeInTheDocument();
    expect(within(row("kp_test_…e5f6")).getByText("Expired")).toBeInTheDocument();
    expect(within(row("kp_test_…g7h8")).getByText("Revoked")).toBeInTheDocument();
  });

  it("offers rotate only on active keys and revoke on anything not revoked", async () => {
    mockApi({ [DETAIL]: { app: app() }, [`GET ${KEYS}`]: page([apiKey(), expired, revoked]) });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await screen.findByText("kp_test_…g7h8");
    expect(screen.queryByRole("button", { name: "Actions for key Leaked (kp_test_…g7h8)" })).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Actions for key Old CI (kp_test_…e5f6)" }));
    let menu = await screen.findByRole("menu");
    expect(within(menu).queryByRole("menuitem", { name: "Rotate" })).not.toBeInTheDocument();
    expect(within(menu).getByRole("menuitem", { name: "Revoke" })).toBeInTheDocument();
    await user.keyboard("{Escape}");

    await user.click(screen.getByRole("button", { name: "Actions for key Default (kp_test_…a1b2)" }));
    menu = await screen.findByRole("menu");
    expect(within(menu).getByRole("menuitem", { name: "Rotate" })).toBeInTheDocument();
    expect(within(menu).getByRole("menuitem", { name: "Revoke" })).toBeInTheDocument();
  });

  it("creates a key with a future expiry and reveals it once", async () => {
    const created = {
      key: used,
      api_key: "kp_test_00000000000000000000000000c3d4",
      api_secret: "kps_test_new-secret",
    };
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([apiKey()]),
      [`POST ${KEYS}`]: () => json(201, created),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Create key" }));
    const dialog = await screen.findByRole("dialog", { name: "Create API key" });
    await user.click(within(dialog).getByRole("button", { name: "Create key" }));
    expect(await within(dialog).findByText("Enter a label.")).toBeInTheDocument();

    await user.type(within(dialog).getByLabelText("Label"), "Production server");
    const expires = within(dialog).getByLabelText("Expires at (WIB, optional)");
    await user.type(expires, "2020-01-01T10:00");
    await user.click(within(dialog).getByRole("button", { name: "Create key" }));
    expect(await within(dialog).findByText("Choose a time in the future.")).toBeInTheDocument();
    expect(api.to("POST", KEYS)).toHaveLength(0);

    await user.clear(expires);
    await user.type(expires, "2099-12-31T23:30");
    await user.click(within(dialog).getByRole("button", { name: "Create key" }));
    const secret = await screen.findByRole("dialog", { name: "Save the new API key" });
    expect(within(secret).getByText(created.api_key)).toBeInTheDocument();
    expect(within(secret).getByText(created.api_secret)).toBeInTheDocument();
    // WIB is UTC+7.
    expect(await api.to("POST", KEYS)[0]!.json()).toEqual({
      label: "Production server",
      expires_at: "2099-12-31T16:30:00.000Z",
    });
    expect(within(secret).getByRole("button", { name: "Done" })).toBeDisabled();
  });

  it("sends a null expiry when none is chosen", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([]),
      [`POST ${KEYS}`]: () => json(201, { key: apiKey(), api_key: "kp_test_x", api_secret: "kps_test_y" }),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(within(await screen.findByTestId("empty-state")).getByRole("button", { name: "Create key" }));
    const dialog = await screen.findByRole("dialog", { name: "Create API key" });
    await user.type(within(dialog).getByLabelText("Label"), "CI");
    await user.click(within(dialog).getByRole("button", { name: "Create key" }));
    await screen.findByRole("dialog", { name: "Save the new API key" });
    expect(await api.to("POST", KEYS)[0]!.json()).toEqual({ label: "CI", expires_at: null });
  });

  it("shows the 10-key limit as a toast with the server message", async () => {
    mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([apiKey()]),
      [`POST ${KEYS}`]: () => json(400, errorBody("validation_failed", "an app can have at most 10 usable API keys")),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Create key" }));
    const dialog = await screen.findByRole("dialog", { name: "Create API key" });
    await user.type(within(dialog).getByLabelText("Label"), "Eleventh");
    await user.click(within(dialog).getByRole("button", { name: "Create key" }));
    expect(await screen.findByText("An app can have at most 10 usable API keys.")).toBeInTheDocument();
    await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create API key" })).not.toBeInTheDocument());
  });

  it("rotates after a confirmation and reveals the new key with a revoke hint", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([apiKey()]),
      [`POST ${KEYS}/${KEY_ID}/rotate`]: () => json(201, rotated),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Actions for key Default (kp_test_…a1b2)" }));
    await user.click(await screen.findByRole("menuitem", { name: "Rotate" }));
    const confirm = await screen.findByRole("dialog", { name: "Rotate Default?" });
    expect(confirm).toHaveTextContent("keeps working until you revoke it");
    await user.click(within(confirm).getByRole("button", { name: "Rotate key" }));
    const secret = await screen.findByRole("dialog", { name: "Save the new API key" });
    expect(secret).toHaveTextContent("revoke the old one");
    expect(within(secret).getByText(rotated.api_secret)).toBeInTheDocument();
    expect(api.to("POST", `${KEYS}/${KEY_ID}/rotate`)).toHaveLength(1);
  });

  it("shows a rotate refusal as a toast", async () => {
    mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([apiKey()]),
      [`POST ${KEYS}/${KEY_ID}/rotate`]: () =>
        json(400, errorBody("validation_failed", "an app can have at most 10 usable API keys")),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Actions for key Default (kp_test_…a1b2)" }));
    await user.click(await screen.findByRole("menuitem", { name: "Rotate" }));
    await user.click(within(await screen.findByRole("dialog")).getByRole("button", { name: "Rotate key" }));
    expect(await screen.findByText("An app can have at most 10 usable API keys.")).toBeInTheDocument();
  });

  it("revokes only after the label is typed", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: page([apiKey()]),
      [`POST ${KEYS}/${KEY_ID}/revoke`]: { key: apiKey({ status: "revoked", revoked_at: "2026-02-01T00:00:00Z" }) },
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Actions for key Default (kp_test_…a1b2)" }));
    await user.click(await screen.findByRole("menuitem", { name: "Revoke" }));
    const confirm = await screen.findByRole("dialog", { name: "Revoke Default?" });
    expect(confirm).toHaveTextContent("api_key_revoked");
    const button = within(confirm).getByRole("button", { name: "Revoke" });
    expect(button).toBeDisabled();
    await user.type(within(confirm).getByRole("textbox", { name: "Type Default to confirm" }), "Default");
    await user.click(button);
    await waitFor(() => expect(api.to("POST", `${KEYS}/${KEY_ID}/revoke`)).toHaveLength(1));
    expect(await screen.findByText("Key Default revoked")).toBeInTheDocument();
    // The list is fetched again after the change.
    await waitFor(() => expect(api.to("GET", KEYS).length).toBeGreaterThan(1));
  });

  it("loads more keys", async () => {
    const api = mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: (request: Request) =>
        new URL(request.url).searchParams.get("cursor") === "k2" ? json(200, page([used])) : json(200, page([apiKey()], "k2")),
    });
    const { user } = renderApp(`/apps/${APP_ID}/keys`);
    await user.click(await screen.findByRole("button", { name: "Load more" }));
    expect(await screen.findByText("kp_test_…c3d4")).toBeInTheDocument();
    expect(screen.getByText("kp_test_…a1b2")).toBeInTheDocument();
    const last = api.to("GET", KEYS).at(-1)!;
    expect(new URL(last.url).searchParams.get("cursor")).toBe("k2");
  });

  it("shows the empty state, an error with retry, and loading rows", async () => {
    mockApi({ [DETAIL]: { app: app() }, [`GET ${KEYS}`]: page([]) });
    const first = renderApp(`/apps/${APP_ID}/keys`);
    const empty = await screen.findByTestId("empty-state");
    expect(empty).toHaveTextContent("No API keys");
    expect(within(empty).getByRole("button", { name: "Create key" })).toBeInTheDocument();
    first.unmount();

    let calls = 0;
    mockApi({
      [DETAIL]: { app: app() },
      [`GET ${KEYS}`]: () => {
        calls += 1;
        return calls === 1 ? json(500, errorBody("internal_error", "boom")) : json(200, page([apiKey()]));
      },
    });
    const second = renderApp(`/apps/${APP_ID}/keys`);
    await second.user.click(within(await screen.findByTestId("error-state")).getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("kp_test_…a1b2")).toBeInTheDocument();
    second.unmount();

    mockApi({ [DETAIL]: { app: app() }, [`GET ${KEYS}`]: () => new Promise<Response>(() => {}) });
    renderApp(`/apps/${APP_ID}/keys`);
    expect((await screen.findAllByTestId("skeleton-row")).length).toBeGreaterThan(0);
  });

  it("renders cards at phone width", async () => {
    setViewport("mobile");
    mockApi({ [DETAIL]: { app: app() }, [`GET ${KEYS}`]: page([apiKey()]) });
    renderApp(`/apps/${APP_ID}/keys`);
    const list = await screen.findByRole("list", { name: "API keys" });
    expect(within(list).getByText("kp_test_…a1b2")).toBeInTheDocument();
    expect(within(list).getByRole("button", { name: "Actions for key Default (kp_test_…a1b2)" })).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("hides key actions and Create key without apps:write", async () => {
    mockApi({ "GET /admin/me": me({ role: "viewer" }), [DETAIL]: { app: app() }, [`GET ${KEYS}`]: page([apiKey()]) });
    renderApp(`/apps/${APP_ID}/keys`);
    await screen.findByText("kp_test_…a1b2");
    expect(screen.queryByRole("button", { name: "Create key" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /^Actions for key/ })).not.toBeInTheDocument();
  });
});
