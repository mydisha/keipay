import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { Profiler, useState } from "react";
import { describe, expect, it, vi } from "vitest";
import { ApiError } from "@/shared/api/errors";
import { setViewport } from "@/test/media";
import { DataTable, type DataColumn, type FilterChipGroup } from "./data-table";

interface Row {
  id: string;
  name: string;
  amount: number;
}

const columns: DataColumn<Row>[] = [
  { id: "name", header: "Name", card: "title", cell: (r) => r.name },
  { id: "amount", header: "Amount", align: "right", cell: (r) => String(r.amount) },
  { id: "actions", header: "Actions", card: "actions", cell: (r) => <button type="button">Open {r.name}</button> },
];

const rows: Row[] = [
  { id: "1", name: "Alpha", amount: 10 },
  { id: "2", name: "Beta", amount: 20 },
];

function renderTable(props: Partial<Parameters<typeof DataTable<Row>>[0]> = {}) {
  return render(
    <DataTable<Row>
      caption="Things"
      columns={columns}
      data={rows}
      getRowId={(r) => r.id}
      emptyState={<p>Nothing here</p>}
      {...props}
    />,
  );
}

describe("DataTable (T-23)", () => {
  it("renders rows in a table on desktop", () => {
    setViewport("desktop");
    renderTable();
    const table = screen.getByRole("table", { name: "Things" });
    expect(within(table).getAllByRole("row")).toHaveLength(3);
    expect(within(table).getByText("Alpha")).toBeInTheDocument();
    expect(within(table).getByRole("columnheader", { name: "Amount" })).toHaveClass("text-right");
  });

  it("shows skeleton rows while loading", () => {
    setViewport("desktop");
    renderTable({ data: undefined, isLoading: true, skeletonRows: 4 });
    expect(screen.getAllByTestId("skeleton-row")).toHaveLength(4);
    expect(screen.queryByText("Nothing here")).not.toBeInTheDocument();
  });

  it("settles when a loaded table goes back to loading (data undefined)", async () => {
    setViewport("desktop");
    let renders = 0;
    const table = (data: Row[] | undefined) => (
      <Profiler id="table" onRender={() => (renders += 1)}>
        <DataTable<Row>
          caption="Things"
          columns={columns}
          data={data}
          isLoading={data === undefined}
          getRowId={(r) => r.id}
          emptyState={<p>Nothing here</p>}
        />
      </Profiler>
    );
    const { rerender } = render(table(rows));
    await Promise.resolve();
    rerender(table(undefined));
    // A fresh `[]` per render made TanStack Table reset its page index after
    // every render, forever. act() would throttle that loop to one step per
    // call, so let React run on its own for a moment, as in the browser.
    const actEnvironment = globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean };
    actEnvironment.IS_REACT_ACT_ENVIRONMENT = false;
    try {
      await new Promise((resolve) => setTimeout(resolve, 200));
    } finally {
      actEnvironment.IS_REACT_ACT_ENVIRONMENT = true;
    }
    expect(renders).toBeLessThan(10);
  });

  it("shows the empty state when there are no rows", () => {
    renderTable({ data: [] });
    expect(screen.getByText("Nothing here")).toBeInTheDocument();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
  });

  it("shows the error state with a retry callback", async () => {
    const onRetry = vi.fn();
    renderTable({
      data: undefined,
      error: new ApiError({ status: 500, code: "internal_error", message: "internal server error" }),
      onRetry,
    });
    const alert = screen.getByTestId("error-state");
    expect(alert).toHaveTextContent("Something went wrong");
    await userEvent.click(within(alert).getByRole("button", { name: "Try again" }));
    expect(onRetry).toHaveBeenCalledOnce();
  });

  it("toggles filter chips and clears them", async () => {
    const user = userEvent.setup();
    const changes: (string | null)[] = [];
    function Harness() {
      const [value, setValue] = useState<string | null>(null);
      const group: FilterChipGroup = {
        id: "status",
        label: "Status",
        options: [
          { value: "PAID", label: "Paid" },
          { value: "FAILED", label: "Failed" },
        ],
        value,
        onChange: (next) => {
          changes.push(next);
          setValue(next);
        },
      };
      return (
        <DataTable<Row> caption="Things" columns={columns} data={rows} getRowId={(r) => r.id} emptyState={null} filters={[group]} />
      );
    }
    render(<Harness />);
    const paid = screen.getByRole("button", { name: "Paid" });
    expect(paid).toHaveAttribute("aria-pressed", "false");
    expect(screen.queryByRole("button", { name: "Clear filters" })).not.toBeInTheDocument();

    await user.click(paid);
    expect(paid).toHaveAttribute("aria-pressed", "true");
    await user.click(screen.getByRole("button", { name: "Failed" }));
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "true");
    expect(paid).toHaveAttribute("aria-pressed", "false");
    await user.click(screen.getByRole("button", { name: "Failed" }));
    expect(screen.getByRole("button", { name: "Failed" })).toHaveAttribute("aria-pressed", "false");

    await user.click(paid);
    await user.click(screen.getByRole("button", { name: "Clear filters" }));
    expect(paid).toHaveAttribute("aria-pressed", "false");
    expect(changes).toEqual(["PAID", "FAILED", null, "PAID", null]);
  });

  it("walks cursor pages with Next/Previous and disables them at the ends", async () => {
    setViewport("desktop");
    const user = userEvent.setup();
    const pages: Record<string, { rows: Row[]; next: string | null }> = {
      first: { rows: [rows[0]!], next: "c2" },
      c2: { rows: [rows[1]!], next: "c3" },
      c3: { rows: [{ id: "3", name: "Gamma", amount: 30 }], next: null },
    };
    const requested: (string | null)[] = [];
    function Harness() {
      const [cursor, setCursor] = useState<string | null>(null);
      const page = pages[cursor ?? "first"]!;
      return (
        <DataTable<Row>
          caption="Things"
          columns={columns}
          data={page.rows}
          getRowId={(r) => r.id}
          emptyState={null}
          pagination={{
            nextCursor: page.next,
            onPageChange: (c) => {
              requested.push(c);
              setCursor(c);
            },
          }}
        />
      );
    }
    render(<Harness />);
    const previous = screen.getByRole("button", { name: "Previous" });
    const next = screen.getByRole("button", { name: "Next" });
    expect(screen.getByText("Page 1")).toBeInTheDocument();
    expect(previous).toBeDisabled();
    expect(next).toBeEnabled();

    await user.click(next);
    expect(screen.getByText("Beta")).toBeInTheDocument();
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Gamma")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();

    await user.click(screen.getByRole("button", { name: "Previous" }));
    expect(screen.getByText("Beta")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Previous" }));
    expect(screen.getByText("Alpha")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(requested).toEqual(["c2", "c3", "c2", null]);
  });

  it("returns to the first page when a filter chip changes", async () => {
    const user = userEvent.setup();
    const requested: (string | null)[] = [];
    function Harness() {
      const [cursor, setCursor] = useState<string | null>(null);
      const [status, setStatus] = useState<string | null>(null);
      return (
        <DataTable<Row>
          caption="Things"
          columns={columns}
          data={cursor ? [rows[1]!] : [rows[0]!]}
          getRowId={(r) => r.id}
          emptyState={null}
          filters={[{ id: "s", label: "Status", options: [{ value: "PAID", label: "Paid" }], value: status, onChange: setStatus }]}
          pagination={{
            nextCursor: cursor ? null : "c2",
            onPageChange: (c) => {
              requested.push(c);
              setCursor(c);
            },
          }}
        />
      );
    }
    render(<Harness />);
    await user.click(screen.getByRole("button", { name: "Next" }));
    expect(screen.getByText("Page 2")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Paid" }));
    expect(requested).toEqual(["c2", null]);
    expect(screen.getByText("Page 1")).toBeInTheDocument();
  });

  it("renders cards with the same fields below 768px", () => {
    setViewport("mobile");
    renderTable();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    const list = screen.getByRole("list", { name: "Things" });
    const cards = within(list).getAllByRole("listitem");
    expect(cards).toHaveLength(2);
    const first = cards[0]!;
    expect(within(first).getByText("Alpha")).toBeInTheDocument();
    expect(within(first).getByText("Amount")).toBeInTheDocument();
    expect(within(first).getByText("10")).toBeInTheDocument();
    expect(within(first).getByRole("button", { name: "Open Alpha" })).toBeInTheDocument();
  });

  it("shows skeleton cards while loading on mobile", () => {
    setViewport("mobile");
    renderTable({ data: undefined, isLoading: true, skeletonRows: 3 });
    expect(screen.getAllByTestId("skeleton-card")).toHaveLength(3);
  });

  it("calls onRowClick but not from the actions cell", async () => {
    setViewport("desktop");
    const onRowClick = vi.fn();
    renderTable({ onRowClick });
    await userEvent.click(screen.getByText("Beta"));
    expect(onRowClick).toHaveBeenCalledWith(rows[1]);
    await userEvent.click(screen.getByRole("button", { name: "Open Alpha" }));
    expect(onRowClick).toHaveBeenCalledTimes(1);
  });
});
