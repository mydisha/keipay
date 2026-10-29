import { expect, request as playwrightRequest, test } from "@playwright/test";
import { errorCode, ownerEmail, ownerPassword, requireOwnerCredentials, signIn, signInToDashboard, signOut } from "./helpers";

// Cookie session and CSRF as the browser sees them.

requireOwnerCredentials();

test.use({ viewport: { width: 1280, height: 800 } });

test("a garbage session cookie lands on sign-in with the session-expired message", async ({ page, context, baseURL }) => {
  await context.addCookies([{ name: "keipay_session", value: "not-a-real-session", url: baseURL! }]);
  await page.goto("/apps");
  await expect(page).toHaveURL(/\/login\?reason=session_expired$/);
  await expect(page.getByText("Your session has expired. Sign in again.")).toBeVisible();
  // Signing in again works from there.
  await page.getByLabel("Email").fill(ownerEmail);
  await page.getByLabel("Password", { exact: true }).fill(ownerPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/$/);
});

test("a protected page without a session goes to sign-in without the message", async ({ page }) => {
  await page.goto("/payments");
  await expect(page).toHaveURL(/\/login$/);
  await expect(page.getByRole("button", { name: "Sign in" })).toBeVisible();
  await expect(page.getByText("Your session has expired. Sign in again.")).toHaveCount(0);
});

test("wrong credentials show the generic message", async ({ page }) => {
  await signIn(page, ownerEmail, `${ownerPassword}-wrong`);
  await expect(page.getByText("Incorrect email or password.")).toBeVisible();
  await expect(page).toHaveURL(/\/login$/);
});

test("the session cookie is HttpOnly and nothing about it is in web storage", async ({ page, context }) => {
  await signInToDashboard(page);
  const cookies = await context.cookies();
  const session = cookies.find((c) => c.name === "keipay_session");
  const csrf = cookies.find((c) => c.name === "keipay_csrf");
  expect(session?.httpOnly).toBe(true);
  expect(session?.sameSite).toBe("Lax");
  expect(csrf?.httpOnly).toBe(false);
  const storage = await page.evaluate(() => JSON.stringify({ ...window.localStorage, ...window.sessionStorage }));
  expect(storage).not.toContain(session!.value);
  expect(storage).not.toMatch(/token|session/i);
});

test("an unsafe call without X-CSRF-Token is refused even with a session", async ({ page }) => {
  await signInToDashboard(page);
  const response = await page.request.post("/admin/apps", {
    data: { name: "No CSRF", slug: `nocsrf-${Date.now().toString(36)}`, environment: "test" },
  });
  expect(response.status()).toBe(403);
  expect(await errorCode(response)).toBe("invalid_csrf_token");
});

test("after sign-out the old session cookie no longer works", async ({ page, context, baseURL }) => {
  await signInToDashboard(page);
  const old = (await context.cookies()).find((c) => c.name === "keipay_session");
  expect(old).toBeDefined();
  await signOut(page);

  const replay = await playwrightRequest.newContext({ baseURL });
  const response = await replay.get("/admin/me", { headers: { Cookie: `keipay_session=${old!.value}` } });
  expect(response.status()).toBe(401);
  expect(await errorCode(response)).toBe("session_expired");
  await replay.dispose();
});
