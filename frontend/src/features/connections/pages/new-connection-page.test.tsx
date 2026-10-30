import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { CON_ID, connection, me, provider } from "@/test/fixtures";
import { errorBody, json, mockApi } from "@/test/http";
import { setViewport } from "@/test/media";
import { renderApp } from "@/test/render";

const DETAIL = `GET /admin/connections/${CON_ID}`;
const TEST = `POST /admin/connections/${CON_ID}/test`;
const TESTED_AT = "2026-01-01T00:05:00Z";

function testResponse(result: "ok" | "failed", message: string, warnings: { code: string; message: string }[] = []) {
  return {
    result,
    message,
    warnings,
    tested_at: TESTED_AT,
    connection: connection({
      last_test_status: result,
      last_tested_at: TESTED_AT,
      last_test_error: result === "failed" ? message : null,
    }),
  };
}

beforeEach(() => {
  setViewport("desktop");
  window.localStorage.setItem(storageKeys.mode, "test");
});

afterEach(() => {
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
});

async function bodyOf(request: Request | undefined): Promise<unknown> {
  return request ? JSON.parse(await request.clone().text()) : undefined;
}

function secretInput(label: string) {
  return screen.getByLabelText(new RegExp(label), { selector: "input" });
}

describe("Add connection wizard", () => {
  it("creates, tests, shows the callback URL, saves channels and opens the detail", async () => {
    const api = mockApi({
      "GET /admin/providers": { data: [provider()] },
      "POST /admin/connections": () => json(201, { connection: connection() }),
      [DETAIL]: { connection: connection() },
      [TEST]: testResponse("ok", "Credentials accepted."),
      [`PUT /admin/connections/${CON_ID}/channels`]: { connection: connection() },
    });
    const { user, router } = renderApp("/connections/new");

    // Step 1: the only provider is preselected; the name follows provider and environment.
    const steps = await screen.findByRole("navigation", { name: "Steps" });
    expect(within(steps).getAllByRole("listitem").map((li) => li.textContent)).toEqual([
      "1Step 1 of 5: Provider",
      "2Step 2 of 5: Credentials",
      "3Step 3 of 5: Test",
      "4Step 4 of 5: Webhook",
      "5Step 5 of 5: Channels",
    ]);
    expect(within(steps).getByText("Provider").closest("li")).toHaveAttribute("aria-current", "step");
    expect(screen.getByRole("radio", { name: /Xendit/ })).toBeChecked();
    expect(screen.getByRole("radio", { name: "Test" })).toBeChecked();
    expect(screen.getByLabelText("Name")).toHaveValue("xendit-test");
    await user.click(screen.getByRole("radio", { name: "Live" }));
    expect(screen.getByLabelText("Name")).toHaveValue("xendit-live");
    await user.click(screen.getByRole("radio", { name: "Test" }));
    await user.click(screen.getByRole("button", { name: "Next" }));

    // Step 2: credentials, then settings under Advanced settings.
    expect(await screen.findByRole("heading", { level: 2, name: "Credentials" })).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Create and test" }));
    expect(await screen.findAllByText("This field is required.")).toHaveLength(2);
    expect(api.to("POST", "/admin/connections")).toHaveLength(0);

    await user.type(secretInput("Secret API key"), "xnd_development_abc123");
    await user.type(secretInput("Webhook verification token"), "tok-12345678");
    const advanced = screen.getByRole("button", { name: "Advanced settings" });
    expect(advanced).toHaveAttribute("aria-expanded", "false");
    await user.click(advanced);
    await user.type(screen.getByLabelText("Invoice lifetime (seconds)"), "600");
    await user.click(screen.getByRole("button", { name: "Create and test" }));

    await waitFor(() => expect(router.state.location.search).toBe(`?connection=${CON_ID}&step=3`));
    expect(await bodyOf(api.to("POST", "/admin/connections")[0])).toEqual({
      provider: "xendit",
      name: "xendit-test",
      environment: "test",
      credentials: { secret_key: "xnd_development_abc123", callback_token: "tok-12345678" },
      settings: { invoice_duration: 600 },
    });

    // Step 3: tested once automatically.
    const outcome = await screen.findByTestId("test-outcome");
    expect(outcome).toHaveTextContent("Passed");
    expect(outcome).toHaveTextContent("Credentials accepted.");
    expect(api.to("POST", `/admin/connections/${CON_ID}/test`)).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Continue" }));

    // Step 4: the callback URL with copy and the provider's steps.
    await waitFor(() => expect(router.state.location.search).toBe(`?connection=${CON_ID}&step=4`));
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    expect(screen.getByTestId("callback-url")).toHaveTextContent(`https://pay.example.com/webhooks/xendit/${CON_ID}`);
    expect(screen.getByText("Paste the URL into Invoices paid.")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: /Open the gateway dashboard/ })).toHaveAttribute(
      "href",
      "https://dashboard.example.com/settings/webhooks",
    );
    await user.click(screen.getByRole("button", { name: "Copy Callback URL" }));
    expect(writeText).toHaveBeenCalledWith(`https://pay.example.com/webhooks/xendit/${CON_ID}`);
    await user.click(screen.getByRole("button", { name: "Continue" }));

    // Step 5: every channel on by default; saving sends the full list.
    expect(await screen.findByRole("checkbox", { name: "QRIS" })).toBeChecked();
    await user.click(screen.getByRole("checkbox", { name: "OVO" }));
    await user.click(screen.getByRole("button", { name: "Save channels" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/connections/${CON_ID}`));
    expect(await bodyOf(api.to("PUT", `/admin/connections/${CON_ID}/channels`)[0])).toEqual({
      channels: [
        { channel: "QRIS", enabled: true },
        { channel: "VA_BCA", enabled: true },
        { channel: "EWALLET_OVO", enabled: false },
      ],
    });
    expect(await screen.findByText("xendit-test is ready")).toBeInTheDocument();
  });

  it("goes back to step 1 with the name error on 409", async () => {
    mockApi({
      "GET /admin/providers": { data: [provider()] },
      "POST /admin/connections": () => json(409, errorBody("conflict", "connection name already exists")),
    });
    const { user } = renderApp("/connections/new");
    await user.click(await screen.findByRole("button", { name: "Next" }));
    await user.type(secretInput("Secret API key"), "xnd_development_abc123");
    await user.type(secretInput("Webhook verification token"), "tok-12345678");
    await user.click(screen.getByRole("button", { name: "Create and test" }));
    expect(await screen.findByText("A connection with this name already exists.")).toBeInTheDocument();
    expect(screen.getByRole("heading", { level: 2, name: "Choose a gateway" })).toBeInTheDocument();
    expect(screen.getByLabelText("Name")).toHaveAttribute("aria-invalid", "true");
  });

  it("shows server field errors from the create request on step 2", async () => {
    mockApi({
      "GET /admin/providers": { data: [provider()] },
      "POST /admin/connections": () =>
        json(
          400,
          errorBody("invalid_request", "invalid request", {
            fields: [
              { field: "credentials.secret_key", rule: "environment" },
              { field: "settings.invoice_duration", rule: "minimum" },
            ],
          }),
        ),
    });
    const { user } = renderApp("/connections/new");
    await user.click(await screen.findByRole("button", { name: "Next" }));
    await user.type(secretInput("Secret API key"), "xnd_production_abc123");
    await user.type(secretInput("Webhook verification token"), "tok-12345678");
    await user.click(screen.getByRole("button", { name: "Create and test" }));
    expect(await screen.findByText("This key is for the other environment.")).toBeInTheDocument();
    // The settings panel opens to show its error.
    expect(screen.getByRole("button", { name: "Advanced settings" })).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("Use 60 or more.")).toBeVisible();
  });

  it("after a failed test can test again and edit only the filled credentials", async () => {
    let tests = 0;
    const api = mockApi({
      "GET /admin/providers": { data: [provider()] },
      [DETAIL]: { connection: connection() },
      [TEST]: () => {
        tests += 1;
        return json(
          200,
          tests < 3
            ? testResponse("failed", "The provider rejected the credentials (HTTP 401).")
            : testResponse("ok", "Credentials accepted."),
        );
      },
      [`PATCH /admin/connections/${CON_ID}/credentials`]: { connection: connection() },
    });
    const { user } = renderApp(`/connections/new?connection=${CON_ID}&step=3`);
    const outcome = await screen.findByTestId("test-outcome");
    expect(outcome).toHaveTextContent("Failed");
    expect(outcome).toHaveTextContent("The provider rejected the credentials (HTTP 401).");
    expect(screen.getByText(/You can continue, but checkouts/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();

    await user.click(screen.getByRole("button", { name: "Test again" }));
    await waitFor(() => expect(tests).toBe(2));
    expect(await screen.findByTestId("test-outcome")).toHaveTextContent("Failed");

    await user.click(screen.getByRole("button", { name: "Edit credentials" }));
    expect(screen.getByText("Credentials").closest("li")).toHaveAttribute("aria-current", "step");
    expect(secretInput("Secret API key")).toHaveAttribute("placeholder", "•••• a1b2");
    await user.type(secretInput("Webhook verification token"), "new-token-123");
    await user.click(screen.getByRole("button", { name: "Save and test" }));
    await waitFor(() => expect(tests).toBe(3));
    expect(await bodyOf(api.to("PATCH", `/admin/connections/${CON_ID}/credentials`)[0])).toEqual({
      credentials: { callback_token: "new-token-123" },
    });
    expect(await screen.findByTestId("test-outcome")).toHaveTextContent("Passed");
  });

  it("shows the warnings of a passed test", async () => {
    mockApi({
      "GET /admin/providers": { data: [provider()] },
      [DETAIL]: { connection: connection() },
      [TEST]: testResponse("ok", "Credentials accepted.", [
        { code: "transactions_unavailable", message: "The provider refused to list transactions with these credentials, so reconciliation runs will fail. Give the key read access to transactions." },
      ]),
    });
    renderApp(`/connections/new?connection=${CON_ID}&step=3`);
    const outcome = await screen.findByTestId("test-outcome");
    expect(outcome).toHaveTextContent("Passed");
    expect(within(outcome).getByTestId("test-warning")).toHaveTextContent("reconciliation runs will fail");
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("resumes on the step in the URL after a reload", async () => {
    const api = mockApi({
      "GET /admin/providers": { data: [provider()] },
      [DETAIL]: { connection: connection({ last_test_status: "failed", last_tested_at: TESTED_AT }) },
    });
    const { user, router } = renderApp(`/connections/new?connection=${CON_ID}&step=4`);
    expect(await screen.findByRole("heading", { level: 2, name: "Set up the callback URL" })).toBeInTheDocument();
    expect(screen.getByText("Webhook").closest("li")).toHaveAttribute("aria-current", "step");
    expect(api.to("POST", `/admin/connections/${CON_ID}/test`)).toHaveLength(0);
    await user.click(screen.getByRole("button", { name: "Skip for now" }));
    await waitFor(() => expect(router.state.location.pathname).toBe(`/connections/${CON_ID}`));
  });

  it("shows a stored result on the test step without testing again", async () => {
    const api = mockApi({
      "GET /admin/providers": { data: [provider()] },
      [DETAIL]: {
        connection: connection({
          last_test_status: "failed",
          last_tested_at: TESTED_AT,
          last_test_error: "Could not reach the provider. Try again later.",
        }),
      },
    });
    renderApp(`/connections/new?connection=${CON_ID}&step=3`);
    expect(await screen.findByTestId("test-outcome")).toHaveTextContent("Could not reach the provider.");
    expect(api.to("POST", `/admin/connections/${CON_ID}/test`)).toHaveLength(0);
  });

  it("starts at the environment in the link", async () => {
    mockApi({ "GET /admin/providers": { data: [provider()] } });
    renderApp("/connections/new?environment=live");
    expect(await screen.findByRole("radio", { name: "Live" })).toBeChecked();
    expect(screen.getByLabelText("Name")).toHaveValue("xendit-live");
  });

  it("asks for a provider when there are several", async () => {
    mockApi({ "GET /admin/providers": { data: [provider(), provider({ name: "acmepay", display_name: "Acme Pay" })] } });
    const { user } = renderApp("/connections/new");
    expect(await screen.findByRole("radio", { name: /Acme Pay/ })).not.toBeChecked();
    expect(screen.getByLabelText("Name")).toHaveValue("");
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(await screen.findByText("Choose a provider.")).toBeInTheDocument();
    await user.click(screen.getByRole("radio", { name: /Acme Pay/ }));
    expect(screen.getByLabelText("Name")).toHaveValue("acmepay-test");
  });

  it("says when no providers are available", async () => {
    mockApi({ "GET /admin/providers": { data: [] } });
    renderApp("/connections/new");
    expect(await screen.findByText("No providers are available.")).toBeInTheDocument();
  });

  it("shows Connection not found for an unknown connection in the URL", async () => {
    mockApi({ "GET /admin/providers": { data: [provider()] } });
    renderApp("/connections/new?connection=con_nope&step=3");
    expect(await screen.findByText("Connection not found")).toBeInTheDocument();
  });

  it("shows ForbiddenState to viewers", async () => {
    const api = mockApi({ "GET /admin/me": me({ role: "viewer" }), "GET /admin/providers": { data: [provider()] } });
    renderApp("/connections/new");
    expect(await screen.findByTestId("forbidden-state")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Next" })).not.toBeInTheDocument();
    expect(api.to("GET", "/admin/providers")).toHaveLength(0);
  });
});
