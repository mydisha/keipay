import { expect, test, type Page } from "@playwright/test";
import { expectAuditAction, expectNoHorizontalScroll, requireOwnerCredentials, settled, signInToDashboard } from "./helpers";

// The Reconciliation page against the real server. scripts/ci.sh starts
// it with a worker and a fake Xendit key on the `xendit-imported` test
// connection, so a run can't reach a real gateway: it must end Failed with a
// generic message and offer Re-run. The success path is covered by Go tests.

requireOwnerCredentials();

/** Every page in this spec starts in test mode, where the imported connection lives. */
async function useTestMode(page: Page) {
  await page.addInitScript(() => window.localStorage.setItem("keipay.mode", "test"));
}

async function expectRunFailed(page: Page) {
  // The worker retries a gateway that can't be reached before giving up.
  const failed = page.getByTestId("run-failed");
  await expect(failed).toBeVisible({ timeout: 45_000 });
  await expect(failed).toContainText("This run failed");
  await expect(failed).toContainText(/Error code: provider_(rejected|unavailable)/);
  await expect(page.getByText("Failed", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Re-run" })).toBeVisible();
}

// Serial: the mobile pass opens the run the desktop pass started.
test.describe.serial("reconciliation", () => {
  test.describe("desktop", () => {
    test.use({ viewport: { width: 1280, height: 800 } });

    test("the summary of a period without payments shows the empty state", async ({ page }) => {
      await useTestMode(page);
      await signInToDashboard(page);
      await page.goto("/reconciliation?from=2020-01-01&to=2020-01-31");
      await expect(page.getByRole("heading", { level: 1, name: "Reconciliation" })).toBeVisible();
      await settled(page);
      await expect(page.getByText("No paid payments in this period")).toBeVisible();
      await expect(page.getByText("Showing test data. The page follows the Test/Live switch.")).toBeVisible();
      await expect(page.getByLabel("From (WIB)")).toHaveValue("2020-01-01");

      // An end before the start is caught before any request.
      await page.getByLabel("To (WIB)").fill("2019-12-01");
      await expect(page.getByRole("alert").filter({ hasText: "The end date can't be before the start date." })).toBeVisible();
      await expect(page.getByRole("link", { name: "Export CSV" })).toHaveCount(0);

      await page.getByRole("group", { name: "Period" }).getByRole("button", { name: "This month" }).click();
      await settled(page);
      await expect(page.getByRole("link", { name: "Export CSV" })).toHaveAttribute("href", /\/admin\/payment-reports\/export\?/);
    });

    test("a run on the test connection fails with its message and can be re-run", async ({ page }) => {
      test.setTimeout(150_000);
      await useTestMode(page);
      await signInToDashboard(page);
      await page.goto("/reconciliation?tab=runs");
      await settled(page);

      await page.getByRole("button", { name: "Run reconciliation" }).first().click();
      const dialog = page.getByRole("dialog", { name: "Run reconciliation" });
      await dialog.getByRole("combobox", { name: "Connection" }).click();
      await page.getByRole("option", { name: /^xendit-imported/ }).click();
      await dialog.getByRole("button", { name: "Start run" }).click();

      await expect(page).toHaveURL(/\/reconciliation\/runs\/rec_[0-9A-Z]{26}$/);
      const runId = new URL(page.url()).pathname.split("/").pop()!;
      await expect(page.getByRole("heading", { level: 1, name: runId })).toBeVisible();
      await expectRunFailed(page);

      await page.getByRole("button", { name: "Re-run" }).click();
      const confirm = page.getByRole("dialog", { name: "Re-run this reconciliation?" });
      await confirm.getByRole("button", { name: "Re-run" }).click();
      await expect(page.getByText("Run queued again")).toBeVisible();
      await expectRunFailed(page);
      await expect(page.locator("dt", { hasText: /^Runs$/ }).locator("xpath=following-sibling::dd[1]")).toHaveText("2");

      // The runs list shows it with a text status.
      await page.getByRole("link", { name: "Back to reconciliation" }).click();
      await settled(page);
      const row = page.getByRole("row").filter({ hasText: runId });
      await expect(row.getByText("Failed", { exact: true })).toBeVisible();

      await expectAuditAction(page, "reconciliation_run.create");
      await expectAuditAction(page, "reconciliation_run.rerun");
    });
  });

  test.describe("mobile 375x812", () => {
    test.use({ viewport: { width: 375, height: 812 }, hasTouch: true });

    test("renders cards without horizontal scroll", async ({ page }) => {
      await useTestMode(page);
      await signInToDashboard(page);

      await page.goto("/reconciliation");
      await settled(page);
      await expectNoHorizontalScroll(page, "/reconciliation");

      await page.goto("/reconciliation?tab=runs");
      await settled(page);
      await expect(page.getByTestId("data-cards")).toBeVisible();
      await expect(page.locator("table")).toHaveCount(0);
      await expectNoHorizontalScroll(page, "/reconciliation?tab=runs");

      await page.getByRole("link", { name: /^View run rec_/ }).first().click();
      await expect(page).toHaveURL(/\/reconciliation\/runs\/rec_[0-9A-Z]{26}$/);
      await settled(page);
      await expect(page.getByRole("button", { name: "Re-run" })).toBeVisible();
      await expectNoHorizontalScroll(page, "run detail");

      await page.getByRole("button", { name: "Re-run" }).click();
      const confirm = page.getByRole("dialog", { name: "Re-run this reconciliation?" });
      await expect(confirm).toBeVisible();
      await expectNoHorizontalScroll(page, "re-run dialog");
      await confirm.getByRole("button", { name: "Cancel" }).click();

      await page.goto("/reconciliation?tab=runs");
      await settled(page);
      await page.getByRole("button", { name: "Run reconciliation" }).first().click();
      await expect(page.getByRole("dialog", { name: "Run reconciliation" })).toBeVisible();
      await expectNoHorizontalScroll(page, "run dialog");
    });
  });
});
