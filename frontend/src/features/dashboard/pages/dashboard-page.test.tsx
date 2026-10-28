import { screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { renderApp } from "@/test/render";

describe("Coming-soon pages", () => {
  it("dashboard points to payments", async () => {
    renderApp("/");
    expect(await screen.findByText("The dashboard is coming soon")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View payments" })).toHaveAttribute("href", "/payments");
  });

  it("connections points to apps", async () => {
    renderApp("/connections");
    expect(await screen.findByText("Connections are coming soon")).toBeInTheDocument();
    expect(screen.getByRole("link", { name: "View apps" })).toHaveAttribute("href", "/apps");
  });
});
