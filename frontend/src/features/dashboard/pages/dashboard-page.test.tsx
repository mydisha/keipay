import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "@/test/render";

describe("Coming-soon pages", () => {
  it("dashboard points to payments", async () => {
    renderApp("/");
    expect(await screen.findByText("The dashboard is coming soon")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View payments" })).toHaveAttribute("href", "/payments");
  });
});
