import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { SecretRevealDialog } from "./secret-reveal-dialog";

const secrets = [
  { label: "API key", value: "kei_live_key" },
  { label: "API secret", value: "s3cr3t-value" },
];

describe("SecretRevealDialog", () => {
  it("cannot close until 'I have saved this' is checked", async () => {
    const user = userEvent.setup();
    const onClose = vi.fn();
    render(<SecretRevealDialog open title="Save these credentials" description="Shown once." secrets={secrets} onClose={onClose} />);

    expect(screen.getByRole("dialog", { name: "Save these credentials" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Close" })).not.toBeInTheDocument();
    const done = screen.getByRole("button", { name: "Done" });
    expect(done).toBeDisabled();

    await user.keyboard("{Escape}");
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: "I have saved this" }));
    expect(done).toBeEnabled();
    await user.click(done);
    expect(onClose).toHaveBeenCalledOnce();
  });

  it("shows every secret with its own copy button and a warning", async () => {
    const user = userEvent.setup();
    const writeText = vi.fn(() => Promise.resolve());
    Object.defineProperty(navigator, "clipboard", { value: { writeText }, configurable: true });
    render(<SecretRevealDialog open title="Save" description="Shown once." secrets={secrets} onClose={vi.fn()} />);

    expect(screen.getByText("kei_live_key")).toBeInTheDocument();
    expect(screen.getByText("s3cr3t-value")).toBeInTheDocument();
    expect(screen.getByRole("note")).toHaveTextContent(/only time the secret is shown/);
    await user.click(screen.getByRole("button", { name: "Copy API secret" }));
    expect(writeText).toHaveBeenCalledWith("s3cr3t-value");
    await user.click(screen.getByRole("button", { name: "Copy API key" }));
    expect(writeText).toHaveBeenLastCalledWith("kei_live_key");
  });

  it("starts unchecked each time it opens", async () => {
    const user = userEvent.setup();
    const { rerender } = render(<SecretRevealDialog open title="Save" description="d" secrets={secrets} onClose={vi.fn()} />);
    await user.click(screen.getByRole("checkbox"));
    rerender(<SecretRevealDialog open={false} title="Save" description="d" secrets={secrets} onClose={vi.fn()} />);
    rerender(<SecretRevealDialog open title="Save" description="d" secrets={secrets} onClose={vi.fn()} />);
    expect(screen.getByRole("checkbox")).not.toBeChecked();
    expect(screen.getByRole("button", { name: "Done" })).toBeDisabled();
  });
});
