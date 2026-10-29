import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Fact, FactList } from "./fact-list";

function factOf(label: string) {
  const dt = screen.getByText(label);
  expect(dt.tagName).toBe("DT");
  return dt;
}

describe("FactList", () => {
  it.each(["rows", "inline", "tiles"] as const)("renders each %s fact as <div><dt/><dd/></div>", (layout) => {
    render(
      <FactList layout={layout}>
        <Fact label="Slug">acme</Fact>
        <Fact label="Created">today</Fact>
      </FactList>,
    );
    const dt = factOf("Slug");
    expect(dt.parentElement?.tagName).toBe("DIV");
    expect(dt.nextElementSibling?.tagName).toBe("DD");
    expect(dt.nextElementSibling).toHaveTextContent("acme");
    expect(dt.parentElement?.parentElement?.tagName).toBe("DL");
  });

  it("renders mono values in the mono face and keeps the label in the list column widths", () => {
    render(
      <FactList labelWidth="sm">
        <Fact label="Id" mono>
          app_1
        </Fact>
      </FactList>,
    );
    const dd = factOf("Id").nextElementSibling;
    expect(dd).toHaveClass("font-mono");
    expect(factOf("Id").parentElement).toHaveClass("sm:grid-cols-[9rem_minmax(0,1fr)]");
  });

  it("renders an empty value and a list without facts", () => {
    const { container } = render(
      <>
        <FactList>
          <Fact label="Note">{null}</Fact>
        </FactList>
        <FactList data-testid="empty" />
      </>,
    );
    expect(factOf("Note").nextElementSibling).toBeEmptyDOMElement();
    expect(screen.getByTestId("empty")).toBeEmptyDOMElement();
    expect(container.querySelectorAll("dl")).toHaveLength(2);
  });
});
