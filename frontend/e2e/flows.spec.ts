import { expect, test, type Page } from "@playwright/test";
import { expectNoHorizontalScroll, requireOwnerCredentials, settled, signInToDashboard, signOut } from "./helpers";

// End-to-end checks for the flows the smoke test only visits: the audited app
// lifecycle (create, edit, deactivate, rotate), the shell controls and the
// 375px layout in both themes. Runs as the owner the setup project created
// and never changes their password, so spec files can run in any order.

requireOwnerCredentials();

const allPaths = [
  "/",
  "/apps",
  "/connections",
  "/webhooks",
  "/payments",
  "/reconciliation",
  "/settings",
  "/settings/users",
  "/settings/audit-log",
];

// Saves the revealed credentials dialog: it cannot be dismissed before the
// "I have saved this" box is ticked.
async function acknowledgeSecret(page: Page) {
  const dialog = page.getByRole("dialog", { name: "Save these credentials" });
  await expect(dialog).toBeVisible();
  await expect(dialog.getByText("API secret", { exact: true })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Copy API secret" })).toBeVisible();
  const done = dialog.getByRole("button", { name: "Done" });
  await expect(done).toBeDisabled();
  await page.keyboard.press("Escape");
  await expect(dialog).toBeVisible();
  await dialog.getByRole("checkbox", { name: "I have saved this" }).check();
  await expect(done).toBeEnabled();
  await done.click();
  await expect(dialog).toBeHidden();
}

async function openRowActions(page: Page, name: string) {
  await page.getByRole("button", { name: `Actions for ${name}`, exact: true }).click();
}

test.describe("apps lifecycle", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("create, edit, deactivate, activate and rotate are each audited", async ({ page }) => {
    const suffix = Date.now().toString(36);
    const slug = `qa-${suffix}`;
    const name = `QA Flow ${suffix}`;
    const renamed = `${name} renamed`;

    await signInToDashboard(page);
    await page.goto("/apps");
    await settled(page);

    // Create: client validation first, then the one-time secret.
    await page.getByRole("button", { name: "Create app" }).first().click();
    const create = page.getByRole("dialog", { name: "Create app" });
    await create.getByRole("button", { name: "Create app" }).click();
    await expect(create.getByText("Enter a name.")).toBeVisible();
    await create.getByLabel("Name").fill(name);
    await create.getByLabel("Slug").fill(slug);
    await create.getByRole("button", { name: "Create app" }).click();
    await acknowledgeSecret(page);
    await expect(page.getByText(name, { exact: true })).toBeVisible();

    // A duplicate slug is reported on the field, not as a crash.
    await page.getByRole("button", { name: "Create app" }).first().click();
    const dup = page.getByRole("dialog", { name: "Create app" });
    await dup.getByLabel("Name").fill(`${name} again`);
    await dup.getByLabel("Slug").fill(slug);
    await dup.getByRole("button", { name: "Create app" }).click();
    await expect(dup.getByText("This slug is already in use.")).toBeVisible();
    await dup.getByRole("button", { name: "Cancel" }).click();
    await expect(dup).toBeHidden();

    // Edit the name.
    await openRowActions(page, name);
    await page.getByRole("menuitem", { name: "Edit" }).click();
    const edit = page.getByRole("dialog", { name: `Edit ${name}` });
    await edit.getByLabel("Name").fill(renamed);
    await edit.getByRole("button", { name: "Save changes" }).click();
    await expect(edit).toBeHidden();
    await expect(page.getByText(renamed, { exact: true })).toBeVisible();

    // Deactivate (confirmed), then activate again.
    await openRowActions(page, renamed);
    await page.getByRole("menuitem", { name: "Deactivate" }).click();
    const deactivate = page.getByRole("dialog", { name: `Deactivate ${renamed}?` });
    await deactivate.getByRole("button", { name: "Deactivate" }).click();
    await expect(deactivate).toBeHidden();
    const row = page.getByRole("row").filter({ hasText: renamed });
    await expect(row.getByText("Inactive", { exact: true })).toBeVisible();
    await openRowActions(page, renamed);
    await page.getByRole("menuitem", { name: "Activate" }).click();
    await expect(row.getByText("Active", { exact: true })).toBeVisible();

    // Rotate behind type-the-slug confirmation, then the new secret once.
    await openRowActions(page, renamed);
    await page.getByRole("menuitem", { name: "Rotate credentials" }).click();
    const rotate = page.getByRole("dialog", { name: `Rotate credentials for ${renamed}?` });
    const confirm = rotate.getByRole("button", { name: "Rotate credentials" });
    await expect(confirm).toBeDisabled();
    const typed = rotate.getByLabel(`Type ${slug} to confirm`);
    await typed.fill(slug.toUpperCase());
    await expect(confirm).toBeDisabled();
    await typed.fill(slug);
    await expect(confirm).toBeEnabled();
    await confirm.click();
    await acknowledgeSecret(page);

    // Settings lists every mutation, newest first, with before/after JSON.
    await page.goto("/settings/audit-log");
    await settled(page);
    const audit = page.locator("section[aria-labelledby=audit-log-heading]");
    const actions = await audit.locator("tbody tr td:nth-child(2)").allInnerTexts();
    expect(actions.slice(0, 5)).toEqual([
      "tenant.rotate_credentials",
      "tenant.update",
      "tenant.update",
      "tenant.update",
      "tenant.create",
    ]);
    await audit.getByRole("button", { name: "View tenant.rotate_credentials" }).first().click();
    const entry = page.getByRole("dialog", { name: "tenant.rotate_credentials" });
    await expect(entry).toBeVisible();
    await expect(entry.getByText("Before", { exact: true })).toBeVisible();
    await expect(entry.getByText("After", { exact: true })).toBeVisible();
    await expect(entry.getByText(/api_key_last4/).first()).toBeVisible();
    await expect(entry.getByText(/api_secret/)).toHaveCount(0);
    await expect(entry.getByText(/^admin:usr_[0-9A-Z]{26}$/)).toBeVisible();
  });
});

test.describe("shell", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("test mode, theme and sidebar state persist; the palette navigates", async ({ page }) => {
    await signInToDashboard(page);
    const banner = page.getByText(/This switch only changes the console for now/);
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
      await expect(page.locator("table")).toHaveCount(0);
      await expect(page.getByText("Demo App").first()).toBeVisible();
      if (theme === "dark") await expect(page.locator("html")).toHaveClass(/\bdark\b/);
      await signOut(page);
      await expectNoHorizontalScroll(page, `/login (${theme})`);
    });
  }
});
