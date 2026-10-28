import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";
import { ConfirmDialog } from "./confirm-dialog";

function renderDialog(confirmText: string | undefined, onConfirm = vi.fn()) {
  render(
    <ConfirmDialog
      open
      onOpenChange={vi.fn()}
      title="Rotate credentials?"
      description="The old key stops working."
      confirmLabel="Rotate"
      confirmText={confirmText}
      destructive
      onConfirm={onConfirm}
    />,
  );
  return onConfirm;
}

describe("ConfirmDialog (T-25)", () => {
  it("keeps confirm disabled until the exact text is typed", async () => {
    const user = userEvent.setup();
    const onConfirm = renderDialog("acme-store");
    const confirm = screen.getByRole("button", { name: "Rotate" });
    const input = screen.getByRole("textbox", { name: "Type acme-store to confirm" });
    expect(confirm).toBeDisabled();

    await user.type(input, "Acme-store");
    expect(confirm).toBeDisabled();
    await user.clear(input);
    await user.type(input, "acme-store ");
    expect(confirm).toBeDisabled();
    await user.clear(input);
    await user.type(input, "acme-store");
    expect(confirm).toBeEnabled();

    await user.click(confirm);
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("matches a unicode slug exactly", async () => {
    const user = userEvent.setup();
    const onConfirm = renderDialog("toko-kué");
    const confirm = screen.getByRole("button", { name: "Rotate" });
    await user.type(screen.getByRole("textbox"), "toko-kue");
    expect(confirm).toBeDisabled();
    await user.clear(screen.getByRole("textbox"));
    await user.type(screen.getByRole("textbox"), "toko-kué{Enter}");
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("confirms directly when no text is required", async () => {
    const onConfirm = renderDialog(undefined);
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
    await userEvent.click(screen.getByRole("button", { name: "Rotate" }));
    expect(onConfirm).toHaveBeenCalledOnce();
  });

  it("does not submit on Enter before the text matches", async () => {
    const user = userEvent.setup();
    const onConfirm = renderDialog("acme");
    await user.type(screen.getByRole("textbox"), "acm{Enter}");
    expect(onConfirm).not.toHaveBeenCalled();
  });
});
