import { expect, test, type Page } from "@playwright/test";

// End-to-end checks for the flows the smoke test only visits: the audited app
// lifecycle (create, edit, deactivate, rotate), the expired-session redirect,
// the shell controls and the 375px layout in both themes. Runs against the
// same server and seeded admin as smoke.spec.ts and never changes the admin
// password, so the two files can run in any order.

const username = process.env.E2E_USERNAME ?? "admin";
const password = process.env.E2E_PASSWORD ?? "";

test.skip(!password, "E2E_PASSWORD is not set; run through `make e2e` or set E2E_BASE_URL, E2E_USERNAME and E2E_PASSWORD");

const TOKEN_KEY = "kei_admin_token";

const allPaths = ["/", "/apps", "/connections", "/webhooks", "/payments", "/reconciliation", "/settings"];

async function signIn(page: Page) {
  await page.goto("/login");
  await page.getByLabel("Username").fill(username);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/$/);
}

async function settled(page: Page) {
  await expect(page.locator("[aria-busy=true]")).toHaveCount(0);
  await expect(page.getByTestId("error-state")).toHaveCount(0);
}

async function expectNoHorizontalScroll(page: Page, label: string) {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(scrollWidth, `${label} scrolls horizontally`).toBeLessThanOrEqual(clientWidth);
}

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

    await signIn(page);
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
    await page.goto("/settings");
    await settled(page);
    const audit = page.getByRole("region", { name: "Audit log" }).or(page.locator("section[aria-labelledby=audit-log-heading]"));
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
    await expect(entry.getByText(`admin:${username}`).or(entry.getByText(username, { exact: true })).first()).toBeVisible();
  });
});

test.describe("session", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("an invalid token sends the user to login with the session-expired message", async ({ page }) => {
    await signIn(page);
    await page.evaluate((key) => window.localStorage.setItem(key, "not-a-valid-token"), TOKEN_KEY);
    await page.goto("/apps");
    await expect(page).toHaveURL(/\/login\?reason=session_expired$/);
    await expect(page.getByText("Your session has expired. Sign in again.")).toBeVisible();
    expect(await page.evaluate((key) => window.localStorage.getItem(key), TOKEN_KEY)).toBeNull();
    // Signing in again works from there.
    await page.getByLabel("Username").fill(username);
    await page.getByLabel("Password", { exact: true }).fill(password);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page).toHaveURL(/\/$/);
  });

  test("a protected page without a token goes to login", async ({ page }) => {
    await page.goto("/payments");
    await expect(page).toHaveURL(/\/login/);
    await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  });

  test("wrong credentials show the invalid-credentials message", async ({ page }) => {
    await page.goto("/login");
    await page.getByLabel("Username").fill(username);
    await page.getByLabel("Password", { exact: true }).fill(`${password}-wrong`);
    await page.getByRole("button", { name: "Sign in" }).click();
    await expect(page.getByText("Invalid username or password")).toBeVisible();
    await expect(page).toHaveURL(/\/login$/);
  });
});

test.describe("shell", () => {
  test.use({ viewport: { width: 1280, height: 800 } });

  test("test mode, theme and sidebar state persist; the palette navigates", async ({ page }) => {
    await signIn(page);
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
      await signIn(page);
      for (const path of allPaths) {
        await page.goto(path);
        await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
        await settled(page);
        await expectNoHorizontalScroll(page, `${path} (${theme})`);
      }
      // Tables become cards on narrow screens.
      await page.goto("/apps");
      await settled(page);
      await expect(page.locator("table")).toHaveCount(0);
      await expect(page.getByText("Demo App").first()).toBeVisible();
      if (theme === "dark") await expect(page.locator("html")).toHaveClass(/\bdark\b/);
    });
  }
});
