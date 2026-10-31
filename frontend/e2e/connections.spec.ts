import { expect, test, type Page } from "@playwright/test";
import {
  createAppInConsole,
  errorCode,
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  showAllEnvironments,
  signInToDashboard,
} from "./helpers";

// Gateway connections against the real server. scripts/ci.sh
// starts it with fake XENDIT_* values, which the server imports once as the
// `xendit-imported` test connection; its key is fake, so every connection
// test against Xendit ends `Failed`. Names are unique per run so the suite can
// run again against the same database.

requireOwnerCredentials();

const importedToken = process.env.XENDIT_CALLBACK_TOKEN ?? "ci-callback-token";
const suffix = Date.now().toString(36);
const connectionName = `e2e-${suffix}`;
const secretKey = `xnd_development_e2e${suffix}`;
const callbackToken = `cb-${suffix}-token`;

/** Filled in by the wizard test; the later tests in this serial group use it. */
const created = { id: "", callbackUrl: "" };

async function openConnection(page: Page, id: string) {
  await page.goto(`/connections/${id}`);
  await expect(page.getByRole("heading", { level: 1, name: connectionName })).toBeVisible();
  await settled(page);
}

async function chooseDefault(page: Page, option: string, appName: string) {
  const select = page.getByRole("combobox", { name: "Default connection" });
  await select.click();
  await page.getByRole("option", { name: option, exact: true }).click();
  await expect(page.getByText(`Default connection of ${appName} updated`)).toBeVisible();
  await expect(select).toHaveText(option);
}

async function confirmDisable(page: Page) {
  await page.getByRole("button", { name: "Disable", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: `Disable ${connectionName}?` });
  await expect(dialog.getByRole("button", { name: "Disable" })).toBeDisabled();
  await dialog.getByRole("textbox").fill(connectionName);
  await dialog.getByRole("button", { name: "Disable" }).click();
}

test.describe.serial("connections", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("the imported connection is listed with its legacy URL", async ({ page }) => {
    await signInToDashboard(page);
    await page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Connections", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: "Connections" })).toBeVisible();
    await settled(page);
    await showAllEnvironments(page);
    const row = page.getByRole("row").filter({ has: page.getByRole("link", { name: "xendit-imported", exact: true }) });
    await expect(row).toHaveCount(1);
    await expect(row.getByText("Legacy URL")).toBeVisible();
    await expect(row.getByText("Test", { exact: true })).toBeVisible();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();
    await expect(row.getByText("Xendit", { exact: true })).toBeVisible();
  });

  test("the wizard creates, tests and sets up a connection without showing the key again", async ({ page }) => {
    test.setTimeout(120_000);
    await signInToDashboard(page);
    await page.goto("/connections");
    await settled(page);
    await page.getByRole("link", { name: "Add connection" }).first().click();
    await expect(page.getByRole("heading", { level: 1, name: "Add connection" })).toBeVisible();

    // Step 1: provider, environment, name. With more than one provider none
    // is chosen for the operator.
    await expect(page.getByRole("radio", { name: /DOKU/ })).not.toBeChecked();
    await page.getByRole("radio", { name: /Xendit/ }).click();
    await expect(page.getByRole("radio", { name: /Xendit/ })).toBeChecked();
    await page.getByRole("radio", { name: "Test", exact: true }).click();
    await page.getByLabel("Name").fill(connectionName);
    await page.getByRole("button", { name: "Next" }).click();

    // Step 2: credentials from the provider's schema.
    await expect(page.getByRole("heading", { level: 2, name: "Credentials" })).toBeVisible();
    const keyInput = page.getByRole("textbox", { name: "Secret API key", exact: true });
    await expect(keyInput).toHaveAttribute("type", "password");
    await keyInput.fill(secretKey);
    await page.getByRole("textbox", { name: "Webhook verification token", exact: true }).fill(callbackToken);
    await page.getByRole("button", { name: "Create and test" }).click();

    // Step 3: the automatic test fails, since the key is fake.
    await expect(page).toHaveURL(/\/connections\/new\?connection=con_[0-9A-Z]{26}&step=3$/);
    created.id = new URL(page.url()).searchParams.get("connection") ?? "";
    const outcome = page.getByTestId("test-outcome");
    await expect(outcome).toContainText("Failed", { timeout: 30_000 });
    await expect(outcome).toContainText(/rejected the credentials|Could not reach the provider/);
    await page.getByRole("button", { name: "Continue" }).click();

    // Step 4: the connection's own callback URL.
    await expect(page).toHaveURL(/step=4$/);
    const url = page.getByTestId("callback-url");
    await expect(url).toHaveText(new RegExp(`/webhooks/xendit/${created.id}$`));
    created.callbackUrl = (await url.innerText()).trim();
    await expect(page.getByRole("button", { name: "Copy Callback URL" })).toBeVisible();
    await page.getByRole("button", { name: "Continue" }).click();

    // Step 5: channels, all on by default.
    await expect(page).toHaveURL(/step=5$/);
    const qris = page.getByRole("checkbox", { name: "QRIS" });
    await expect(qris).toBeChecked();
    await qris.click();
    await page.getByRole("button", { name: "Save channels" }).click();

    // The detail page: hints only, never the key.
    await expect(page).toHaveURL(new RegExp(`/connections/${created.id}$`));
    await expect(page.getByRole("heading", { level: 1, name: connectionName })).toBeVisible();
    await settled(page);
    await expect(page.getByText(`•••• ${secretKey.slice(-4)}`)).toBeVisible();
    await expect(page.getByRole("checkbox", { name: "QRIS" })).not.toBeChecked();
    await expect(page.getByRole("region", { name: "Overview" }).getByText("Failed")).toBeVisible();
    expect(await page.content()).not.toContain(secretKey);
    expect(await page.content()).not.toContain(callbackToken);

    // Reloading keeps the hints and still never shows the values.
    await page.reload();
    await settled(page);
    expect(await page.content()).not.toContain(secretKey);
  });

  test("an app's default connection blocks disabling until it is cleared", async ({ page }) => {
    test.skip(!created.id, "the wizard test did not create a connection");
    const appName = `E2E Conn ${suffix}`;
    await signInToDashboard(page);
    const app = await createAppInConsole(page, { name: appName, environment: "Test" });

    await expect(page.getByText("Payments for this app fail until a default connection is set.")).toBeVisible();
    await chooseDefault(page, connectionName, appName);
    await expect(page.getByText("Payments for this app fail until a default connection is set.")).toHaveCount(0);

    await openConnection(page, created.id);
    await confirmDisable(page);
    const inUse = page.getByRole("dialog", { name: "Connection in use" });
    await expect(inUse).toBeVisible();
    await expect(inUse.getByRole("link", { name: appName })).toHaveAttribute("href", `/apps/${app.id}`);
    await inUse.getByRole("button", { name: "Close" }).first().click();
    await expect(page.getByRole("heading", { level: 1 }).locator("..").getByText("Active", { exact: true })).toBeVisible();

    await page.goto(`/apps/${app.id}`);
    await expect(page.getByRole("heading", { level: 1, name: appName })).toBeVisible();
    await settled(page);
    await chooseDefault(page, "None", appName);

    await openConnection(page, created.id);
    await confirmDisable(page);
    await expect(page.getByText(`${connectionName} disabled`)).toBeVisible();
    await expect(page.getByRole("button", { name: "Enable" })).toBeVisible();

    await page.getByRole("button", { name: "Enable" }).click();
    const enable = page.getByRole("dialog", { name: `Enable ${connectionName}?` });
    await enable.getByRole("button", { name: "Enable" }).click();
    await expect(page.getByText(`${connectionName} enabled`)).toBeVisible();
    await expect(page.getByRole("button", { name: "Disable", exact: true })).toBeVisible();
  });

  test("callbacks are verified with the connection's own token", async ({ request }) => {
    test.skip(!created.id, "the wizard test did not create a connection");
    const path = new URL(created.callbackUrl).pathname;
    expect(path).toBe(`/webhooks/xendit/${created.id}`);
    const body = {
      id: `inv_e2e_${suffix}`,
      external_id: `kp_pay_unknown_${suffix}`,
      status: "PAID",
      amount: 10000,
      currency: "IDR",
    };

    const foreign = await request.post(path, { data: body, headers: { "x-callback-token": importedToken } });
    expect(foreign.status()).toBe(401);
    expect(await errorCode(foreign)).toBe("invalid_callback_token");

    const own = await request.post(path, { data: body, headers: { "x-callback-token": callbackToken } });
    expect(own.status()).toBe(500);

    const malformed = await request.post("/webhooks/xendit/con_nope", {
      data: body,
      headers: { "x-callback-token": callbackToken },
    });
    expect(malformed.status()).toBe(404);
    expect(await errorCode(malformed)).toBe("not_found");
  });
});

test.describe("connections at 375px", () => {
  test.use({ viewport: { width: 375, height: 812 }, hasTouch: true });

  test("the list is cards and the wizard fits without horizontal scroll", async ({ page }) => {
    await signInToDashboard(page);
    await page.goto("/connections");
    await settled(page);
    await showAllEnvironments(page);
    await expect(page.locator("table")).toHaveCount(0);
    await expect(page.getByTestId("data-cards")).toBeVisible();
    await expect(page.getByRole("link", { name: "xendit-imported", exact: true })).toBeVisible();
    await expectNoHorizontalScroll(page, "/connections");

    await page.goto("/connections/new");
    await expect(page.getByRole("heading", { level: 1, name: "Add connection" })).toBeVisible();
    await settled(page);
    await expect(page.getByRole("radio", { name: /Xendit/ })).toBeVisible();
    await expectNoHorizontalScroll(page, "/connections/new");
  });
});
