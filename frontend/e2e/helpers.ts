import { expect, test, type APIResponse, type Browser, type Page } from "@playwright/test";

// Fixed by scripts/ci.sh: the owner the `setup` project creates.
export const ownerEmail = process.env.E2E_EMAIL ?? "";
export const ownerPassword = process.env.E2E_PASSWORD ?? "";

/** Call at the top of every spec file. */
export function requireOwnerCredentials() {
  test.skip(
    !ownerEmail || !ownerPassword,
    "E2E_EMAIL and E2E_PASSWORD are not set; run through `make e2e` or set E2E_BASE_URL, E2E_EMAIL and E2E_PASSWORD",
  );
}

/** A second browser (another person) against the same server; config options are not inherited otherwise. */
export async function newBrowserContext(browser: Browser, viewport = { width: 1280, height: 800 }) {
  return browser.newContext({ baseURL: test.info().project.use.baseURL, viewport });
}

export async function signIn(page: Page, email = ownerEmail, password = ownerPassword) {
  await page.goto("/login");
  await page.getByLabel("Email").fill(email);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
}

export async function signInToDashboard(page: Page, email = ownerEmail, password = ownerPassword) {
  await signIn(page, email, password);
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { level: 1, name: "Dashboard" })).toBeVisible();
}

export async function signOut(page: Page) {
  await page.getByRole("button", { name: "Account menu" }).click();
  await page.getByRole("menuitem", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
}

/** Every list has finished loading and nothing failed. */
export async function settled(page: Page) {
  await expect(page.locator("[aria-busy=true]")).toHaveCount(0);
  await expect(page.getByTestId("error-state")).toHaveCount(0);
}

export async function expectNoHorizontalScroll(page: Page, label: string) {
  const { scrollWidth, clientWidth } = await page.evaluate(() => ({
    scrollWidth: document.documentElement.scrollWidth,
    clientWidth: document.documentElement.clientWidth,
  }));
  expect(scrollWidth, `${label} scrolls horizontally`).toBeLessThanOrEqual(clientWidth);
}

/** Pages through `/settings/audit-log` until an entry with this action shows. */
export async function expectAuditAction(page: Page, action: string) {
  await page.goto("/settings/audit-log");
  await settled(page);
  const entries = page.locator("section[aria-labelledby=audit-log-heading]");
  for (;;) {
    const actions = await entries.locator("tbody tr td:nth-child(2)").allInnerTexts();
    if (actions.includes(action)) return;
    const next = page.getByRole("button", { name: "Next" });
    if ((await next.count()) === 0 || (await next.isDisabled())) break;
    await next.click();
    await settled(page);
  }
  throw new Error(`audit log has no ${action} entry`);
}

export async function errorCode(response: APIResponse): Promise<string | undefined> {
  const body = (await response.json().catch(() => null)) as { error?: { code?: string } } | null;
  return body?.error?.code;
}
