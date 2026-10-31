import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { toast } from "sonner";
import { describe, expect, it } from "vitest";
import { storageKeys } from "@/shared/lib/storage";
import { ThemeProvider } from "@/shared/lib/theme";
import { Toaster } from "./sonner";

function renderToaster() {
  const user = userEvent.setup();
  const result = render(
    <ThemeProvider>
      <Toaster />
    </ThemeProvider>,
  );
  return { ...result, user };
}

/** The toast element sonner renders for the message `text`. */
async function findToast(text: string) {
  const message = await screen.findByText(text);
  return message.closest<HTMLElement>("[data-sonner-toast]")!;
}

describe("Toaster", () => {
  it("shows a toast with a close button that dismisses it", async () => {
    const { user } = renderToaster();
    act(() => {
      toast("Saved the endpoint");
    });
    const item = await findToast("Saved the endpoint");
    const close = item.querySelector<HTMLButtonElement>("button[data-close-button]");
    expect(close).not.toBeNull();
    expect(close).toHaveAccessibleName(/close/i);
    await user.click(close!);
    await waitFor(() => expect(screen.queryByText("Saved the endpoint")).not.toBeInTheDocument());
  });

  it("sets toasts in the console font with the close button in the corner", async () => {
    renderToaster();
    act(() => {
      toast("Copied");
    });
    const item = await findToast("Copied");
    expect(item.className).toContain("font-sans!");
    expect(item.querySelector("button[data-close-button]")?.className).toContain("right-2.5!");
  });

  it("tints the icon by type and keeps the type in the words", async () => {
    renderToaster();
    act(() => {
      toast.success("Key rotated");
      toast.error("Couldn't save");
      toast.warning("Retry scheduled");
      toast.info("Nothing changed");
    });
    const icon = async (text: string) => (await findToast(text)).querySelector("[data-icon] svg");
    expect(await icon("Key rotated")).toHaveClass("text-success-foreground");
    expect(await icon("Couldn't save")).toHaveClass("text-danger-foreground");
    expect(await icon("Retry scheduled")).toHaveClass("text-warning-foreground");
    expect(await icon("Nothing changed")).toHaveClass("text-muted-foreground");
    // Icons are decoration; the message carries the meaning.
    expect(await icon("Couldn't save")).toHaveAttribute("aria-hidden", "true");
  });

  it("follows the console theme", async () => {
    window.localStorage.setItem(storageKeys.theme, "dark");
    renderToaster();
    act(() => {
      toast("Dark toast");
    });
    await findToast("Dark toast");
    expect(document.querySelector("[data-sonner-toaster]")).toHaveAttribute("data-sonner-theme", "dark");
  });

  it("renders nothing while no toast is active", () => {
    renderToaster();
    expect(document.querySelector("[data-sonner-toast]")).toBeNull();
  });
});
