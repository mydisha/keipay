import { expect, test, type Page } from "@playwright/test";
import {
  acknowledgeSecrets,
  createAppInConsole,
  expectAuditAction,
  expectNoHorizontalScroll,
  requireOwnerCredentials,
  settled,
  showAllEnvironments,
  signInToDashboard,
  signOut,
} from "./helpers";

// End-to-end checks for the flows the smoke test only visits: the audited app
// lifecycle (create, edit, keys, disable), the shell controls and the 375px
// layout in both themes. Runs as the owner the setup project created
// and never changes their password, so spec files can run in any order.

requireOwnerCredentials();

const allPaths = [
  "/",
  "/apps",
  "/connections",
  "/connections/new",
  "/webhooks",
  "/payments",
  "/reconciliation",
  "/settings",
  "/settings/users",
  "/settings/audit-log",
];

function keyRow(page: Page, apiKey: string) {
  return page.getByRole("row").filter({ hasText: `…${apiKey.slice(-4)}` });
}

async function keyAction(page: Page, label: string, apiKey: string, action: "Rotate" | "Revoke") {
  await page.getByRole("button", { name: `Actions for key ${label} (${apiKey.slice(0, 8)}…${apiKey.slice(-4)})` }).click();
  await page.getByRole("menuitem", { name: action }).click();
}

test.describe("apps lifecycle", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("create, edit, keys, disable and enable are each audited", async ({ page }) => {
    const suffix = Date.now().toString(36);
    const name = `QA Flow ${suffix}`;
    const renamed = `${name} renamed`;

    await signInToDashboard(page);
    await page.goto("/apps");
    await settled(page);

    // Client validation first.
    await page.getByRole("button", { name: "Create app" }).first().click();
    const create = page.getByRole("dialog", { name: "Create app" });
    await create.getByRole("button", { name: "Create app" }).click();
    await expect(create.getByText("Enter a name.")).toBeVisible();
    await create.getByRole("button", { name: "Cancel" }).click();

    // Create a test app: two secrets once, then its detail page.
    const created = await createAppInConsole(page, { name, metadata: '{"team": "qa"}' });
    expect(created.slug).toBe(`qa-flow-${suffix}`);
    expect(created.apiKey).toMatch(/^kp_test_[0-9a-f]{32}$/);
    expect(created.apiSecret).toMatch(/^kps_test_[0-9a-f]{48}$/);
    await expect(page.getByText(created.apiSecret)).toHaveCount(0);
    await expect(page.getByRole("region", { name: "Metadata" })).toContainText('"team": "qa"');

    // The slug is unique per environment: taken in test, free in live.
    await page.goto("/apps");
    await settled(page);
    await page.getByRole("button", { name: "Create app" }).first().click();
    const dup = page.getByRole("dialog", { name: "Create app" });
    await dup.getByLabel("Name").fill(name);
    await dup.getByRole("radio", { name: "Test" }).click();
    await dup.getByRole("button", { name: "Create app" }).click();
    await expect(dup.getByText("This slug is already used in the test environment.")).toBeVisible();
    await dup.getByRole("radio", { name: "Live" }).click();
    await dup.getByRole("button", { name: "Create app" }).click();
    await acknowledgeSecrets(page, "Save these credentials", ["API key"]);
    await expect(page.getByRole("heading", { level: 1, name })).toBeVisible();
    await expect(page.getByText("Live", { exact: true }).first()).toBeVisible();

    // Edit the name and metadata of the test app.
    await page.goto(`/apps/${created.id}`);
    await page.getByRole("button", { name: "Edit" }).click();
    const edit = page.getByRole("dialog", { name: `Edit ${name}` });
    await edit.getByLabel("Name").fill(renamed);
    await edit.getByLabel("Metadata").fill('{"team": "qa", "tier": 2}');
    await edit.getByRole("button", { name: "Save changes" }).click();
    await expect(edit).toBeHidden();
    await expect(page.getByRole("heading", { level: 1, name: renamed })).toBeVisible();
    await expect(page.getByRole("region", { name: "Metadata" })).toContainText('"tier": 2');

    // Keys: create a second key, rotate it, revoke the first.
    await page.getByRole("navigation", { name: "App sections" }).getByRole("link", { name: "API keys" }).click();
    await settled(page);
    await expect(keyRow(page, created.apiKey).getByText("Active", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Create key" }).click();
    const keyDialog = page.getByRole("dialog", { name: "Create API key" });
    await keyDialog.getByLabel("Label").fill("Second");
    await keyDialog.getByRole("button", { name: "Create key" }).click();
    const second = (await acknowledgeSecrets(page, "Save the new API key", ["API key", "API secret"]))["API key"]!;
    await expect(keyRow(page, second).getByText("Active", { exact: true })).toBeVisible();

    await keyAction(page, "Second", second, "Rotate");
    await page.getByRole("dialog", { name: "Rotate Second?" }).getByRole("button", { name: "Rotate key" }).click();
    const rotated = (await acknowledgeSecrets(page, "Save the new API key", ["API key", "API secret"]))["API key"]!;
    await expect(keyRow(page, rotated).getByText("Active", { exact: true })).toBeVisible();
    await expect(keyRow(page, second).getByText("Active", { exact: true })).toBeVisible();

    await keyAction(page, "Default", created.apiKey, "Revoke");
    const revoke = page.getByRole("dialog", { name: "Revoke Default?" });
    const revokeButton = revoke.getByRole("button", { name: "Revoke" });
    await expect(revokeButton).toBeDisabled();
    await revoke.getByLabel("Type Default to confirm").fill("Default");
    await revokeButton.click();
    await expect(revoke).toBeHidden();
    await expect(keyRow(page, created.apiKey).getByText("Revoked", { exact: true })).toBeVisible();
    // Only prefix and last four are ever shown again.
    await expect(page.getByText(created.apiKey)).toHaveCount(0);

    // Disable needs the slug typed exactly; enable is a plain confirm.
    await page.getByRole("button", { name: "Disable" }).click();
    const disable = page.getByRole("dialog", { name: `Disable ${renamed}?` });
    const disableButton = disable.getByRole("button", { name: "Disable" });
    await expect(disableButton).toBeDisabled();
    await disable.getByLabel(`Type ${created.slug} to confirm`).fill(created.slug.toUpperCase());
    await expect(disableButton).toBeDisabled();
    await disable.getByLabel(`Type ${created.slug} to confirm`).fill(created.slug);
    await disableButton.click();
    await expect(disable).toBeHidden();
    await expect(page.getByText("Disabled", { exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "Enable" }).click();
    await page.getByRole("dialog", { name: `Enable ${renamed}?` }).getByRole("button", { name: "Enable" }).click();
    await expect(page.getByRole("button", { name: "Disable" })).toBeVisible();

    // Every mutation is in the audit log, and no entry carries a secret.
    for (const action of ["app.create", "app.update", "app.disable", "app.enable", "api_key.create", "api_key.rotate", "api_key.revoke"]) {
      await expectAuditAction(page, action);
    }
    await page.goto("/settings/audit-log");
    await settled(page);
    const audit = page.locator("section[aria-labelledby=audit-log-heading]");
    await audit.getByRole("button", { name: "View api_key.rotate" }).first().click();
    const entry = page.getByRole("dialog", { name: "api_key.rotate" });
    await expect(entry.getByText("Before", { exact: true })).toBeVisible();
    await expect(entry.getByText("After", { exact: true })).toBeVisible();
    await expect(entry.getByText(/kps_|kpwh_/)).toHaveCount(0);
    await expect(entry.getByText(second)).toHaveCount(0);
    await expect(entry.getByText(rotated)).toHaveCount(0);
    await expect(entry.getByText(/^admin:usr_[0-9A-Z]{26}$/)).toBeVisible();
  });
});

test.describe("shell", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("test mode, theme and sidebar state persist; the palette navigates", async ({ page }) => {
    await signInToDashboard(page);
    const banner = page.getByText(
      /The Dashboard, the Apps, Connections and Webhooks lists, Reconciliation, new apps and new connections follow this switch/,
    );
    await expect(banner).toHaveCount(0);

    await page.getByRole("switch", { name: "Test mode" }).click();
    await expect(banner).toBeVisible();

    await page.getByRole("button", { name: "Change theme" }).click();
    await page.getByRole("menuitemradio", { name: "Dark" }).click();
    await expect(page.locator("html")).toHaveClass(/\bdark\b/);

    await page.getByRole("button", { name: "Collapse sidebar" }).click();
    await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeVisible();

    await page.reload();
    await expect(banner).toBeVisible();
    await expect(page.locator("html")).toHaveClass(/\bdark\b/);
    await expect(page.getByRole("button", { name: "Expand sidebar" })).toBeVisible();
    // The banner follows every page.
    await page.goto("/payments");
    await expect(banner).toBeVisible();

    // ⌘K / Ctrl+K opens the palette, which navigates and switches mode back.
    await page.keyboard.press("ControlOrMeta+k");
    const palette = page.getByRole("dialog", { name: "Command palette" });
    await expect(palette).toBeVisible();
    await palette.getByRole("combobox").fill("Reconciliation");
    await page.keyboard.press("Enter");
    await expect(palette).toBeHidden();
    await expect(page).toHaveURL(/\/reconciliation$/);

    await page.keyboard.press("ControlOrMeta+k");
    await palette.getByRole("combobox").fill("live");
    await page.keyboard.press("Enter");
    await expect(banner).toHaveCount(0);

    await page.getByRole("button", { name: "Change theme" }).click();
    await page.getByRole("menuitemradio", { name: "Light" }).click();
    await expect(page.locator("html")).not.toHaveClass(/\bdark\b/);
    await page.getByRole("button", { name: "Expand sidebar" }).click();
  });
});

test.describe("375px", () => {
  test.use({ viewport: { width: 375, height: 812 }, hasTouch: true });

  for (const theme of ["light", "dark"] as const) {
    test(`every page fits at 375px in ${theme} mode`, async ({ page }) => {
      await page.addInitScript((t) => window.localStorage.setItem("keipay.theme", t), theme);
      await signInToDashboard(page);
      for (const path of allPaths) {
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await settled(page);
        await expectNoHorizontalScroll(page, `${path} (${theme})`);
      }
      await page.goto("/change-password");
      await expect(page.getByRole("heading", { level: 1, name: "Change password" })).toBeVisible();
      await expectNoHorizontalScroll(page, `/change-password (${theme})`);
      // Tables become cards on narrow screens.
      await page.goto("/apps");
      await settled(page);
      await showAllEnvironments(page);
      await expect(page.locator("table")).toHaveCount(0);
      await expect(page.getByText("Demo App").first()).toBeVisible();
      if (theme === "dark") await expect(page.locator("html")).toHaveClass(/\bdark\b/);
      await signOut(page);
      await expectNoHorizontalScroll(page, `/login (${theme})`);
    });
  }
});
