import { screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it } from "vitest";
import { app, APP_ID, DLV_ID, EVT_ID, INB_ID, me, page, webhookEndpoint, WHE_ID } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";
import { parseRecordId } from "./command-palette";

// F-11: pasting a webhook record id into the palette opens it.

beforeEach(() => setViewport("desktop"));

async function openPalette() {
  const rendered = renderApp("/");
  await screen.findByRole("heading", { level: 1, name: "Dashboard" });
  await rendered.user.keyboard("{Control>}k{/Control}");
  const dialog = await screen.findByRole("dialog", { name: "Command palette" });
  return { ...rendered, dialog };
}

describe("parseRecordId", () => {
  it.each([
    [DLV_ID, "dlv"],
    [` ${INB_ID} `, "inb"],
    [EVT_ID.toLowerCase(), "evt"],
    [WHE_ID, "whe"],
  ])("recognises %s", (text, kind) => {
    expect(parseRecordId(text)?.kind).toBe(kind);
  });

  it.each(["dlv_123", "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C", "go webhooks", "", `${DLV_ID}x`])("ignores %s", (text) => {
    expect(parseRecordId(text)).toBeNull();
  });
});

describe("Command palette ids (F-11)", () => {
  it.each([
    [DLV_ID, `Open delivery ${DLV_ID}`, `?tab=outbound&id=${DLV_ID}`],
    [INB_ID, `Open inbound webhook ${INB_ID}`, `?tab=inbound&id=${INB_ID}`],
    [EVT_ID, `Show deliveries of event ${EVT_ID}`, `?tab=outbound&event_id=${EVT_ID}`],
  ])("routes %s to the Webhooks page", async (id, label, search) => {
    mockApi({ "GET /admin/webhook-deliveries": page([]), "GET /admin/inbound-webhooks": page([]) });
    const { user, dialog, router } = await openPalette();
    await user.type(within(dialog).getByRole("combobox"), id);
    await user.click(await within(dialog).findByRole("option", { name: label }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/webhooks"));
    expect(router.state.location.search).toBe(search);
  });

  it("looks up a whe_ id and opens its app's Webhooks tab", async () => {
    const api = mockApi({
      [`GET /admin/webhook-endpoints/${WHE_ID}`]: { endpoint: webhookEndpoint() },
      [`GET /admin/apps/${APP_ID}`]: { app: app() },
      [`GET /admin/apps/${APP_ID}/webhook-endpoints`]: page([]),
    });
    const { user, dialog, router } = await openPalette();
    await user.type(within(dialog).getByRole("combobox"), WHE_ID);
    await user.click(await within(dialog).findByRole("option", { name: `Open webhook endpoint ${WHE_ID}` }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/apps/${APP_ID}/webhooks`));
    expect(api.to("GET", `/admin/webhook-endpoints/${WHE_ID}`)).toHaveLength(1);
  });

  it("says so when the endpoint can't be found", async () => {
    mockApi({ [`GET /admin/webhook-endpoints/${WHE_ID}`]: () => json(404, errorBody("not_found", "not found")) });
    const { user, dialog, router } = await openPalette();
    await user.type(within(dialog).getByRole("combobox"), WHE_ID);
    await user.click(await within(dialog).findByRole("option", { name: `Open webhook endpoint ${WHE_ID}` }));
    expect(await screen.findByText("Couldn't open that webhook endpoint. It may have been deleted.")).toBeInTheDocument();
    expect(router.state.location.pathname).toBe("/");
  });

  it("offers no record without the permission and nothing for other text", async () => {
    mockApi({ "GET /admin/me": me({}, ["apps:read"]) });
    const { user, dialog } = await openPalette();
    await user.type(within(dialog).getByRole("combobox"), DLV_ID);
    expect(within(dialog).queryByRole("option", { name: /Open delivery/ })).not.toBeInTheDocument();
    await user.clear(within(dialog).getByRole("combobox"));
    await user.type(within(dialog).getByRole("combobox"), "dlv_short");
    expect(within(dialog).queryByRole("option", { name: /Open delivery/ })).not.toBeInTheDocument();
  });
});
