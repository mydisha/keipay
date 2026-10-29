import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Toaster } from "sonner";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IdText } from "./id-text";

const LONG_ID = "pay_01JBKQ7Z8M4X2Y6V3N9T5R1W0C";

function stubClipboard(writeText: (text: string) => Promise<void>) {
  const spy = vi.fn(writeText);
  Object.defineProperty(navigator, "clipboard", { value: { writeText: spy }, configurable: true });
  return spy;
}

afterEach(() => {
  Object.defineProperty(navigator, "clipboard", { value: undefined, configurable: true });
});

describe("IdText", () => {
  it("is monospace, truncated, and keeps the full id in title", () => {
    render(<IdText value={LONG_ID} label="payment id" />);
    const text = screen.getByText(LONG_ID);
    expect(text).toHaveClass("font-mono", "truncate");
    expect(text).toHaveAttribute("title", LONG_ID);
  });

  it("copies the full id and announces it", async () => {
    // userEvent.setup() installs its own clipboard; stub after it.
    const user = userEvent.setup();
    const writeText = stubClipboard(() => Promise.resolve());
    render(<IdText value={LONG_ID} prefix="#" label="payment id" />);
    await user.click(screen.getByRole("button", { name: `Copy payment id ${LONG_ID}` }));
    expect(writeText).toHaveBeenCalledWith(LONG_ID);
    expect(await screen.findByRole("status")).toHaveTextContent("Copied");
  });

  it("shows an error toast when the clipboard rejects", async () => {
    const user = userEvent.setup();
    stubClipboard(() => Promise.reject(new Error("denied")));
    render(
      <>
        <Toaster />
        <IdText value={42} label="ID" />
      </>,
    );
    await user.click(screen.getByRole("button", { name: "Copy ID 42" }));
    await waitFor(() => expect(screen.getByText(/Couldn't copy to the clipboard/)).toBeInTheDocument());
    expect(screen.getByRole("status")).toHaveTextContent("");
  });
});
