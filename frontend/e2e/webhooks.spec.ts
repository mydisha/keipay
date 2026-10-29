import { readFile, writeFile } from "node:fs/promises";
import { createServer, type IncomingHttpHeaders, type Server } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath, pathToFileURL } from "node:url";
import { expect, test, type Page } from "@playwright/test";
import {
  acknowledgeSecrets,
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  showAllEnvironments,
  signInToDashboard,
} from "./helpers";

// T-E1..T-E3: a local receiver gets a signed test event from an endpoint
// added in the console; the README's Node.js function verifies it, also with
// the two signatures sent after a rotation; wrong-token and unknown-reference
// gateway callbacks show up in the Inbound log. Runs at desktop and phone
// width. Each run adds its own endpoint to the seeded Demo App and deletes it.

requireOwnerCredentials();

const README = fileURLToPath(new URL("../../README.md", import.meta.url));
// The xendit-imported connection's token, set by scripts/ci.sh.
const CALLBACK_TOKEN = "ci-callback-token";

type Verify = (header: string, rawBody: Buffer, secret: string, toleranceSeconds?: number) => boolean;

/** Loads `verifyKeiPaySignature` exactly as README.md prints it, so the docs are what gets tested. */
async function readmeVerifier(outputPath: string): Promise<Verify> {
  const readme = await readFile(README, "utf8");
  const block = /Node\.js \(`rawBody`[^\n]*\n\n```js\n([\s\S]*?)```/.exec(readme)?.[1];
  expect(block, "README.md has the Node.js verification snippet").toBeTruthy();
  await writeFile(outputPath, block!);
  const mod = (await import(pathToFileURL(outputPath).href)) as { verifyKeiPaySignature: Verify };
  return mod.verifyKeiPaySignature;
}

interface Received {
  path: string;
  headers: IncomingHttpHeaders;
  body: Buffer;
}

/** An HTTP receiver on 127.0.0.1 that answers 200 and keeps every request. */
async function startReceiver(): Promise<{ server: Server; port: number; requests: Received[] }> {
  const requests: Received[] = [];
  const server = createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk: Buffer) => chunks.push(chunk));
    req.on("end", () => {
      requests.push({ path: req.url ?? "", headers: req.headers, body: Buffer.concat(chunks) });
      res.writeHead(200, { "Content-Type": "text/plain" }).end("ok");
    });
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  return { server, port: (server.address() as AddressInfo).port, requests };
}

async function demoAppWebhooks(page: Page): Promise<string> {
  await page.goto("/apps");
  await settled(page);
  await showAllEnvironments(page);
  await page.getByRole("link", { name: "Demo App" }).first().click();
  await expect(page).toHaveURL(/\/apps\/app_[0-9A-Z]{26}$/);
  const path = `${new URL(page.url()).pathname}/webhooks`;
  await page.goto(path);
  await expect(page.getByRole("heading", { level: 2, name: "Webhook endpoints" })).toBeVisible();
  await settled(page);
  return path;
}

async function endpointAction(page: Page, url: string, action: string) {
  await page.getByRole("button", { name: `Actions for ${url}` }).click();
  await page.getByRole("menuitem", { name: action }).click();
}

/** Sends a test event from the row menu, follows the toast to the delivery and waits for it to be delivered. */
async function sendTestAndOpenDelivery(page: Page, url: string): Promise<string> {
  await endpointAction(page, url, "Send test event");
  const dialog = page.getByRole("dialog", { name: "Send a test event" });
  await expect(dialog.getByRole("combobox", { name: "Event type" })).toHaveText(/payment\.paid/);
  await dialog.getByRole("button", { name: "Send test event" }).click();
  await expect(page.getByText("Test event queued")).toBeVisible();
  await page.getByRole("button", { name: "View delivery" }).click();
  await expect(page).toHaveURL(/\/webhooks\?tab=outbound&id=dlv_[0-9A-Za-z]{26}$/);
  const deliveryId = new URL(page.url()).searchParams.get("id")!;
  const sheet = page.getByRole("dialog", { name: "Delivery" });
  // The open detail reloads while the delivery is pending.
  await expect(sheet.getByText("Delivered", { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(sheet.getByText("Attempt 1")).toBeVisible();
  await expect(sheet.getByText("HTTP 200")).toBeVisible();
  return deliveryId;
}

function v1Values(header: string): string[] {
  return header
    .split(",")
    .map((part) => part.trim())
    .filter((part) => part.startsWith("v1="));
}

async function openNewestInbound(page: Page, result: string) {
  await page.getByRole("group", { name: "Filter by result" }).getByRole("button", { name: result, exact: true }).click();
  await settled(page);
  await page.getByRole("button", { name: /^View inbound webhook inb_/ }).first().click();
  const sheet = page.getByRole("dialog", { name: "Inbound webhook" });
  await expect(sheet.getByRole("heading", { name: "Raw body" })).toBeVisible();
  return sheet;
}

const viewports = [
  { label: "desktop", viewport: { width: 1280, height: 800 }, hasTouch: false },
  { label: "mobile", viewport: { width: 375, height: 812 }, hasTouch: true },
] as const;

for (const { label, viewport, hasTouch } of viewports) {
  test.describe(`webhook router (${label})`, () => {
    test.use({ viewport, hasTouch });

    test("T-E1/T-E2: add an endpoint, send signed test events, rotate with overlap, delete", async ({ page }, testInfo) => {
      const verify = await readmeVerifier(testInfo.outputPath("verify-keipay-signature.mjs"));
      const receiver = await startReceiver();
      try {
        const suffix = Date.now().toString(36);
        const url = `http://127.0.0.1:${receiver.port}/hooks/${label}-${suffix}`;
        const host = `127.0.0.1:${receiver.port}`;

        await signInToDashboard(page);
        const tabPath = await demoAppWebhooks(page);

        // T-E1: add the endpoint (all events, one custom header) and keep its secret.
        await page.getByRole("button", { name: "Add endpoint" }).first().click();
        const sheet = page.getByRole("dialog", { name: "Add endpoint" });
        await expect(sheet).toBeVisible();
        if (label === "mobile") {
          // The sheet slides in; measure once it has arrived.
          await expect
            .poll(async () => {
              const box = await sheet.boundingBox();
              return box ? [Math.round(box.x), Math.round(box.x + box.width)] : null;
            })
            .toEqual([0, 375]);
        }
        await sheet.getByLabel("Endpoint URL").fill(url);
        await sheet.getByLabel("Description (optional)").fill(`e2e ${label}`);
        await sheet.getByRole("checkbox", { name: "All events" }).click();
        await expect(sheet.getByRole("checkbox", { name: "payment.paid" })).toBeDisabled();
        await sheet.getByRole("button", { name: "Add header" }).click();
        await sheet.getByLabel("Header 1 name").fill("Authorization");
        await sheet.getByLabel("Header 1 value").fill("Bearer e2e-receiver");
        await sheet.getByRole("button", { name: "Add endpoint" }).click();
        const first = (await acknowledgeSecrets(page, "Save the signing secret", ["Signing secret"]))["Signing secret"]!;
        expect(first).toMatch(/^kpwh_[0-9a-f]{48}$/);
        await expect(page.getByText(url).first()).toBeVisible();
        await expect(page.getByText(`…${first.slice(-4)}`).first()).toBeVisible();
        if (label === "mobile") await expectNoHorizontalScroll(page, tabPath);

        // Send a test event; the receiver gets a body the README function verifies.
        const firstDelivery = await sendTestAndOpenDelivery(page, url);
        await expect.poll(() => receiver.requests.length, { timeout: 15_000 }).toBe(1);
        const got = receiver.requests[0]!;
        expect(got.path).toBe(`/hooks/${label}-${suffix}`);
        expect(got.headers["x-keipay-test"]).toBe("true");
        expect(got.headers["x-keipay-event-type"]).toBe("payment.paid");
        expect(got.headers["x-keipay-delivery-id"]).toBe(firstDelivery);
        expect(got.headers["x-keipay-event-id"]).toMatch(/^evt_/);
        expect(got.headers.authorization).toBe("Bearer e2e-receiver");
        expect(got.headers["x-kei-signature"]).toBeUndefined();
        const signature = String(got.headers["x-keipay-signature"]);
        expect(v1Values(signature)).toHaveLength(1);
        expect(verify(signature, got.body, first)).toBe(true);
        expect(verify(signature, Buffer.concat([got.body, Buffer.from(" ")]), first)).toBe(false);
        expect(verify(signature, got.body, `${first.slice(0, -1)}0`)).toBe(false);
        const envelope = JSON.parse(got.body.toString("utf8")) as { id: string; type: string; api_version: string };
        expect(envelope).toMatchObject({ type: "payment.paid", api_version: "v2", id: got.headers["x-keipay-event-id"] });

        // The attempt log shows the custom header redacted.
        const detail = page.getByRole("dialog", { name: "Delivery" });
        await detail.getByText("Request headers").click();
        await expect(detail.getByText("[REDACTED]")).toBeVisible();
        if (label === "mobile") await expectNoHorizontalScroll(page, "delivery detail");
        await page.keyboard.press("Escape");
        await expect(detail).toHaveCount(0);

        // The Outbound list shows it as Delivered (the demo app is a test app).
        await page
          .getByRole("group", { name: "Filter by environment" })
          .getByRole("button", { name: "Test", exact: true })
          .click();
        await settled(page);
        await expect(page.getByRole("button", { name: `View delivery ${firstDelivery}` })).toBeVisible();
        if (label === "mobile") {
          await expect(page.getByTestId("data-cards")).toBeVisible();
          await expectNoHorizontalScroll(page, "/webhooks outbound");
        } else {
          const row = page.getByRole("row").filter({ has: page.getByRole("button", { name: `View delivery ${firstDelivery}` }) });
          await expect(row.getByText("Delivered", { exact: true })).toBeVisible();
          await expect(row.getByText("1/1")).toBeVisible();
        }

        // T-E2: rotate with the default 24 h overlap; the next request carries both signatures.
        await page.goto(tabPath);
        await settled(page);
        await endpointAction(page, url, "Rotate secret");
        const rotate = page.getByRole("dialog", { name: "Rotate the signing secret?" });
        await expect(rotate.getByLabel("Overlap (hours)")).toHaveValue("24");
        const confirmRotate = rotate.getByRole("button", { name: "Rotate secret" });
        await expect(confirmRotate).toBeDisabled();
        await rotate.getByLabel(`Type ${host} to confirm`).fill(host);
        await confirmRotate.click();
        const second = (await acknowledgeSecrets(page, "Save the signing secret", ["Signing secret"]))["Signing secret"]!;
        expect(second).toMatch(/^kpwh_[0-9a-f]{48}$/);
        expect(second).not.toBe(first);
        await expect(page.getByText(/Old secret also signs until/).first()).toBeVisible();

        await sendTestAndOpenDelivery(page, url);
        await expect.poll(() => receiver.requests.length, { timeout: 15_000 }).toBe(2);
        const rotated = receiver.requests[1]!;
        const twoSigs = String(rotated.headers["x-keipay-signature"]);
        expect(v1Values(twoSigs)).toHaveLength(2);
        expect(verify(twoSigs, rotated.body, second)).toBe(true);
        expect(verify(twoSigs, rotated.body, first)).toBe(true);

        // Delete needs the host typed; the endpoint leaves the list.
        await page.goto(tabPath);
        await settled(page);
        await endpointAction(page, url, "Delete");
        const del = page.getByRole("dialog", { name: "Delete this endpoint?" });
        await expect(del.getByRole("button", { name: "Delete" })).toBeDisabled();
        await del.getByLabel(`Type ${host} to confirm`).fill(host);
        await del.getByRole("button", { name: "Delete" }).click();
        await expect(page.getByText("Endpoint deleted")).toBeVisible();
        await expect(page.getByRole("button", { name: `Actions for ${url}` })).toHaveCount(0);
      } finally {
        await new Promise((resolve) => receiver.server.close(resolve));
      }
    });

    test("T-E3: wrong-token and unknown-reference callbacks are logged in Inbound", async ({ page, request }) => {
      const suffix = `${label}-${Date.now().toString(36)}`;
      const wrongToken = { id: `inv_e2e_bad_${suffix}`, external_id: "kp_pay_unknown", status: "PAID", amount: 1000, currency: "IDR" };
      const unknownRef = { id: `inv_e2e_${suffix}`, external_id: "kp_pay_unknown", status: "PAID", amount: 1000, currency: "IDR" };

      const rejected = await request.post("/webhooks/xendit", {
        headers: { "x-callback-token": "wrong" },
        data: wrongToken,
      });
      expect(rejected.status()).toBe(401);
      const failed = await request.post("/webhooks/xendit", {
        headers: { "x-callback-token": CALLBACK_TOKEN },
        data: unknownRef,
      });
      expect(failed.status()).toBe(500);

      await signInToDashboard(page);
      await page.goto("/webhooks?tab=inbound&environment=all");
      await expect(page.getByRole("tab", { name: "Inbound", selected: true })).toBeVisible();
      await settled(page);
      if (label === "mobile") await expectNoHorizontalScroll(page, "/webhooks inbound");

      const invalid = await openNewestInbound(page, "Invalid signature");
      await expect(invalid.getByText("Invalid signature", { exact: true })).toBeVisible();
      await expect(invalid.getByText("Invalid", { exact: true })).toBeVisible();
      await expect(invalid.getByText(wrongToken.id)).toBeVisible();
      await page.keyboard.press("Escape");
      await expect(invalid).toHaveCount(0);

      // Choosing another result replaces the filter.
      const error = await openNewestInbound(page, "Error");
      await expect(error.getByText("Error", { exact: true })).toBeVisible();
      await expect(error.getByText("Verified", { exact: true })).toBeVisible();
      await expect(error.getByText(unknownRef.id).first()).toBeVisible();
      await expect(error.getByText(/kp_pay_unknown/).first()).toBeVisible();
      if (label === "mobile") await expectNoHorizontalScroll(page, "inbound detail");
    });
  });
}
