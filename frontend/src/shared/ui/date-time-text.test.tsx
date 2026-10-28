import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import { DateTimeText } from "./date-time-text";
import { TooltipProvider } from "./tooltip";

const now = new Date("2026-01-01T18:35:00Z");

function renderText(value: string | null) {
  return render(
    <TooltipProvider delayDuration={0}>
      <DateTimeText value={value} now={now} />
    </TooltipProvider>,
  );
}

describe("DateTimeText (T-21)", () => {
  it("shows relative time from the injected now in a <time> element", () => {
    renderText("2026-01-01T18:30:00Z");
    const time = screen.getByText("5 minutes ago");
    expect(time.tagName).toBe("TIME");
    expect(time).toHaveAttribute("dateTime", "2026-01-01T18:30:00.000Z");
  });

  it("shows the absolute WIB time on focus, on the next Jakarta day", async () => {
    renderText("2026-01-01T18:30:00Z");
    const time = screen.getByText("5 minutes ago");
    expect(time).toHaveAccessibleName(expect.stringMatching(/2 Jan 2026.*01\.30.*WIB$/));
    await userEvent.tab();
    expect(time).toHaveFocus();
    const tooltip = await screen.findByRole("tooltip");
    expect(tooltip).toHaveTextContent(/2 Jan 2026, 01\.30\.00 WIB/);
  });

  it.each([null, "", "garbage"])("renders an em dash for %s", (value) => {
    renderText(value);
    expect(screen.getByText("—")).toBeInTheDocument();
    expect(document.querySelector("time")).toBeNull();
  });
});
