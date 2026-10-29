import { render, screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Section, SectionHeader } from "./section";

describe("Section", () => {
  it("is a region named by its heading, with the id used as given", () => {
    render(
      <Section id="audit-log-heading" title="Audit log" description="Who changed what.">
        <p>rows</p>
      </Section>,
    );
    const region = screen.getByRole("region", { name: "Audit log" });
    expect(region).toHaveAttribute("aria-labelledby", "audit-log-heading");
    expect(within(region).getByRole("heading", { level: 2, name: "Audit log" })).toHaveAttribute("id", "audit-log-heading");
    expect(within(region).getByText("Who changed what.")).toBeInTheDocument();
    expect(within(region).getByText("rows")).toBeInTheDocument();
  });

  it("renders actions next to the heading", () => {
    render(
      <Section id="users-heading" title="Users" actions={<button type="button">Add user</button>}>
        <p>table</p>
      </Section>,
    );
    expect(within(screen.getByRole("region", { name: "Users" })).getByRole("button", { name: "Add user" })).toBeInTheDocument();
  });

  it("uses an h3 for nested sections and omits the description when there is none", () => {
    render(
      <Section id="resolution-heading" title="Resolution" headingLevel={3}>
        <p>form</p>
      </Section>,
    );
    expect(screen.getByRole("heading", { level: 3, name: "Resolution" })).toBeInTheDocument();
    expect(screen.queryByRole("heading", { level: 2 })).not.toBeInTheDocument();
    expect(screen.getByRole("region", { name: "Resolution" }).querySelector("p")).toHaveTextContent("form");
  });

  it("passes other section attributes through", () => {
    render(
      <Section id="h" title="Facts" data-testid="facts">
        <p />
      </Section>,
    );
    expect(screen.getByTestId("facts")).toHaveAttribute("aria-labelledby", "h");
  });
});

describe("SectionHeader", () => {
  it("renders a heading on its own for sections that manage their own markup", () => {
    render(<SectionHeader id="summary-heading" title="Summary" />);
    expect(screen.getByRole("heading", { level: 2, name: "Summary" })).toHaveAttribute("id", "summary-heading");
    expect(screen.queryByRole("region")).not.toBeInTheDocument();
  });
});
