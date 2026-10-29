import { createHmac, randomInt } from "node:crypto";
import { expect, test, type APIRequestContext, type Page } from "@playwright/test";
import {
  acknowledgeSecrets,
  createAppInConsole,
  errorCode,
  requireOwnerCredentials,
  settled,
  signInToDashboard,
} from "./helpers";

// API key state is enforced on /v1 right away. Credentials come from the
// console; requests are signed here like an app would sign them. CI's Xendit
// key is fake, so only GET /v1/payments/{id} is used: a 404 means the key
// passed authentication.

requireOwnerCredentials();

test.use({ viewport: { width: 1280, height: 800 } });

const CROCKFORD = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** A well-formed `pay_` id that no payment has. */
function randomPaymentId() {
  let id = "pay_0";
  for (let i = 0; i < 25; i += 1) id += CROCKFORD[randomInt(CROCKFORD.length)];
  return id;
}

// A signature is accepted once and covers only the timestamp and body, so
// repeated GETs (empty body) need distinct timestamps. Each one is the
// current second or one before the previous, whichever is earlier: never
// repeated, and a handful of requests stays well inside the 5-minute window.
let lastTimestamp = Number.POSITIVE_INFINITY;
function nextTimestamp() {
  lastTimestamp = Math.min(Math.floor(Date.now() / 1000), lastTimestamp - 1);
  return String(lastTimestamp);
}

interface Credentials {
  apiKey: string;
  apiSecret: string;
}

async function getPayment(request: APIRequestContext, { apiKey, apiSecret }: Credentials, tamper = false) {
  const timestamp = nextTimestamp();
  let signature = createHmac("sha256", apiSecret).update(`${timestamp}.`).digest("hex");
  if (tamper) signature = `${signature.slice(0, -1)}${signature.endsWith("0") ? "1" : "0"}`;
  const response = await request.get(`/v1/payments/${randomPaymentId()}`, {
    headers: { "X-Api-Key": apiKey, "X-Timestamp": timestamp, "X-Signature": signature },
  });
  return { status: response.status(), code: await errorCode(response) };
}

async function openKeys(page: Page, appId: string) {
  await page.goto(`/apps/${appId}/keys`);
  await settled(page);
}

async function keyMenu(page: Page, apiKey: string, action: "Rotate" | "Revoke") {
  await page.getByRole("button", { name: new RegExp(`^Actions for key .* \\(${apiKey.slice(0, 8)}…${apiKey.slice(-4)}\\)$`) }).click();
  await page.getByRole("menuitem", { name: action }).click();
}

test("revoked keys, rotation and disabled apps take effect on /v1 at once", async ({ page, request }) => {
  const suffix = Date.now().toString(36);
  await signInToDashboard(page);
  const app = await createAppInConsole(page, { name: `Keys ${suffix}` });
  const first: Credentials = { apiKey: app.apiKey, apiSecret: app.apiSecret };

  // An active key authenticates: the unknown payment is a 404.
  expect(await getPayment(request, first)).toEqual({ status: 404, code: "not_found" });
  // A bad signature fails before any key state is considered.
  expect(await getPayment(request, first, true)).toEqual({ status: 401, code: "invalid_signature" });

  // Rotate: the new key works and the old one keeps working.
  await openKeys(page, app.id);
  await keyMenu(page, first.apiKey, "Rotate");
  await page.getByRole("dialog", { name: "Rotate Default?" }).getByRole("button", { name: "Rotate key" }).click();
  const values = await acknowledgeSecrets(page, "Save the new API key", ["API key", "API secret"]);
  const second: Credentials = { apiKey: values["API key"]!, apiSecret: values["API secret"]! };
  expect(second.apiKey).toMatch(/^kp_test_[0-9a-f]{32}$/);
  expect(await getPayment(request, second)).toEqual({ status: 404, code: "not_found" });
  expect(await getPayment(request, first)).toEqual({ status: 404, code: "not_found" });

  // Revoke the old key: refused immediately, with its own code; the new one still works.
  await keyMenu(page, first.apiKey, "Revoke");
  const revoke = page.getByRole("dialog", { name: "Revoke Default?" });
  await revoke.getByLabel("Type Default to confirm").fill("Default");
  await revoke.getByRole("button", { name: "Revoke" }).click();
  await expect(revoke).toBeHidden();
  expect(await getPayment(request, first)).toEqual({ status: 401, code: "api_key_revoked" });
  // Key state is only reported after a valid signature.
  expect(await getPayment(request, first, true)).toEqual({ status: 401, code: "invalid_signature" });
  expect(await getPayment(request, second)).toEqual({ status: 404, code: "not_found" });

  // Disable the app: every key is refused until it is enabled again.
  await page.getByRole("button", { name: "Disable" }).click();
  const disable = page.getByRole("dialog", { name: `Disable Keys ${suffix}?` });
  await disable.getByLabel(`Type ${app.slug} to confirm`).fill(app.slug);
  await disable.getByRole("button", { name: "Disable" }).click();
  await expect(disable).toBeHidden();
  expect(await getPayment(request, second)).toEqual({ status: 403, code: "tenant_inactive" });

  await page.getByRole("button", { name: "Enable" }).click();
  await page.getByRole("dialog", { name: `Enable Keys ${suffix}?` }).getByRole("button", { name: "Enable" }).click();
  await expect(page.getByRole("button", { name: "Disable" })).toBeVisible();
  expect(await getPayment(request, second)).toEqual({ status: 404, code: "not_found" });

  // The key list shows that the new key was used: "Never" is left only under Expires.
  const row = page.getByRole("row").filter({ hasText: `…${second.apiKey.slice(-4)}` });
  await expect(async () => {
    await openKeys(page, app.id);
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
    await expect(row.getByText("Never", { exact: true })).toHaveCount(1, { timeout: 1000 });
  }).toPass({ timeout: 15_000 });
});
