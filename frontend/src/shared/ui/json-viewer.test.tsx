import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { JSON_PREVIEW_LIMIT, JsonViewer } from "./json-viewer";

describe("JsonViewer", () => {
  it("is collapsed by default and pretty-prints when opened", async () => {
    render(<JsonViewer label="After" value={{ name: "Acme", active: true }} />);
    const toggle = screen.getByRole("button", { name: "After" });
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByText(/"name"/)).not.toBeInTheDocument();
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(document.querySelector("pre")?.textContent).toBe('{\n  "name": "Acme",\n  "active": true\n}');
  });

  it("handles null with a 'No data' hint", () => {
    render(<JsonViewer label="Before" value={null} defaultOpen />);
    expect(screen.getByRole("button", { name: /^Before/ })).toHaveAccessibleName("Before (No data)");
    expect(screen.getByText("null")).toBeInTheDocument();
  });

  it("truncates large payloads in the view but copies everything", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    const big = { data: "x".repeat(JSON_PREVIEW_LIMIT * 2) };
    render(<JsonViewer label="Payload" value={big} defaultOpen />);
    expect(screen.getByText(/Showing the first 20,000 of/)).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Copy Payload JSON" }));
    expect(writeText).toHaveBeenCalledWith(JSON.stringify(big, null, 2));
  });
});
