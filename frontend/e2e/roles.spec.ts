import { expect, test, type Browser, type Page } from "@playwright/test";
import {
  errorCode,
  expectAuditAction,
  newBrowserContext,
  ownerEmail,
  requireOwnerCredentials,
  settled,
  signIn,
  signInToDashboard,
} from "./helpers";

// T-57: the owner adds a viewer and an admin; each sees only what their
// permissions allow; disabling the viewer ends their session at once.

requireOwnerCredentials();

test.use({ viewport: { width: 1280, height: 800 } });

async function openUsers(page: Page) {
  await page.goto("/settings/users");
  await settled(page);
  await expect(page.getByRole("heading", { level: 2, name: "Users" })).toBeVisible();
}

/** Adds a user and returns the temporary password shown once. */
async function addUser(page: Page, email: string, name: string, role: "Admin" | "Viewer") {
  await page.getByRole("button", { name: "Add user" }).first().click();
  const dialog = page.getByRole("dialog", { name: "Add user" });
  await dialog.getByLabel("Email").fill(email);
  await dialog.getByLabel("Name").fill(name);
  await dialog.getByRole("combobox", { name: "Role" }).click();
  await page.getByRole("option", { name: new RegExp(`^${role}`) }).click();
  await dialog.getByRole("button", { name: "Add user" }).click();

  const secret = page.getByRole("dialog", { name: "Save the temporary password" });
  await expect(secret).toBeVisible();
  const password = (await secret.locator("code").innerText()).trim();
  expect(password).toMatch(/^[A-HJ-NP-Za-km-z2-9]{20}$/);
  const done = secret.getByRole("button", { name: "Done" });
  await expect(done).toBeDisabled();
  await secret.getByRole("checkbox", { name: "I have saved this" }).check();
  await done.click();
  await expect(secret).toBeHidden();
  // Shown once: nothing on the page keeps it.
  await expect(page.getByText(password)).toHaveCount(0);
  await expect(page.getByText(email, { exact: true })).toBeVisible();
  return password;
}

/** Signs a new user in from their own browser and replaces the temporary password. */
async function firstSignIn(browser: Browser, email: string, temporary: string, chosen: string) {
  const context = await newBrowserContext(browser);
  const page = await context.newPage();
  await signIn(page, email, temporary);
  await expect(page).toHaveURL(/\/change-password$/);
  await expect(page.getByRole("heading", { level: 1, name: "Set a new password" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Back to the console" })).toHaveCount(0);
  // The console stays locked until the password is changed.
  await page.goto("/apps");
  await expect(page).toHaveURL(/\/change-password$/);

  await page.getByLabel("Current password", { exact: true }).fill(temporary);
  await page.getByLabel("New password", { exact: true }).fill(chosen);
  await page.getByLabel("Confirm new password").fill(chosen);
  await page.getByRole("button", { name: "Change password" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();
  return { context, page };
}

function settingsTabs(page: Page) {
  return page.getByRole("navigation", { name: "Settings sections" }).getByRole("link");
}

test("roles shape the console and disabling ends a session", async ({ page, browser }) => {
  const suffix = Date.now().toString(36);
  const viewerEmail = `viewer-${suffix}@example.com`;
  const adminEmail = `admin-${suffix}@example.com`;
  const chosen = `chosen password ${suffix}`;

  await signInToDashboard(page);
  await openUsers(page);
  const viewerTemp = await addUser(page, viewerEmail, `Viewer ${suffix}`, "Viewer");
  const adminTemp = await addUser(page, adminEmail, `Admin ${suffix}`, "Admin");

  // The owner can't disable or reset their own row.
  await page.getByRole("button", { name: `Actions for ${ownerEmail}`, exact: true }).click();
  const ownMenu = page.getByRole("menu");
  await expect(ownMenu.getByRole("menuitem", { name: "Edit" })).toBeVisible();
  await expect(ownMenu.getByRole("menuitem", { name: "Disable" })).toHaveCount(0);
  await expect(ownMenu.getByRole("menuitem", { name: "Reset password" })).toHaveCount(0);
  await page.keyboard.press("Escape");

  // Viewer: read-only console.
  const viewer = await firstSignIn(browser, viewerEmail, viewerTemp, chosen);
  await viewer.page.goto("/apps");
  await settled(viewer.page);
  await expect(viewer.page.getByText("Demo App").first()).toBeVisible();
  await expect(viewer.page.getByRole("button", { name: "Create app" })).toHaveCount(0);
  await expect(viewer.page.getByRole("button", { name: /^Actions for/ })).toHaveCount(0);
  await viewer.page.goto("/settings");
  await expect(settingsTabs(viewer.page)).toHaveText(["Account"]);
  await viewer.page.goto("/settings/users");
  await expect(viewer.page.getByTestId("forbidden-state")).toBeVisible();
  await expect(viewer.page).toHaveURL(/\/settings\/users$/);
  const forbidden = await viewer.page.request.get("/admin/users");
  expect(forbidden.status()).toBe(403);
  expect(await errorCode(forbidden)).toBe("forbidden");

  // Admin: audit log but no user management.
  const admin = await firstSignIn(browser, adminEmail, adminTemp, chosen);
  await admin.page.goto("/settings");
  await expect(settingsTabs(admin.page)).toHaveText(["Account", "Audit log"]);
  await admin.page.goto("/settings/users");
  await expect(admin.page.getByTestId("forbidden-state")).toBeVisible();
  await admin.context.close();

  // The owner disables the viewer, typing the email to confirm.
  await openUsers(page);
  await page.getByRole("button", { name: `Actions for ${viewerEmail}`, exact: true }).click();
  await page.getByRole("menuitem", { name: "Disable" }).click();
  const confirm = page.getByRole("dialog", { name: `Disable Viewer ${suffix}?` });
  const disable = confirm.getByRole("button", { name: "Disable" });
  await expect(disable).toBeDisabled();
  await confirm.getByLabel(`Type ${viewerEmail} to confirm`).fill(viewerEmail);
  await disable.click();
  await expect(confirm).toBeHidden();
  const row = page.getByRole("row").filter({ hasText: viewerEmail });
  await expect(row.getByText("Disabled", { exact: true })).toBeVisible();

  // The viewer's next navigation ends on sign-in with the message.
  await viewer.page.getByRole("navigation", { name: "Main" }).getByRole("link", { name: "Payments" }).click();
  await expect(viewer.page).toHaveURL(/\/login\?reason=session_expired$/);
  await expect(viewer.page.getByText("Your session has expired. Sign in again.")).toBeVisible();
  await viewer.context.close();

  await expectAuditAction(page, "user.create");
  await expectAuditAction(page, "user.disable");
});
