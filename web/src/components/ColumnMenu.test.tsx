import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import ColumnMenu from "./ColumnMenu";

// The regression this guards: the column panel was a daisyUI dropdown rendered
// in-flow inside the grid card, which has overflow-hidden, so a short grid clipped
// the panel. The panel must float over the grid, which means it must mount OUTSIDE
// any overflow-clipping ancestor (it portals to the document body).
const columns = { type: { label: "Type" }, parent: { label: "Parent" }, tech: { label: "Technical name" } };

describe("ColumnMenu", () => {
  it("floats the column panel outside an overflow-clipping ancestor", async () => {
    const { getByLabelText, container } = render(() => (
      <div data-testid="clip" style={{ overflow: "hidden" }}>
        <ColumnMenu
          columns={columns}
          columnKeys={["type", "parent", "tech"]}
          cols={() => ["type", "parent"]}
          onToggle={() => {}}
          onMove={() => {}}
        />
      </div>
    ));
    fireEvent.click(getByLabelText("Columns"));
    const panel = await screen.findByText(/drag to reorder/i);
    const clip = container.querySelector('[data-testid="clip"]') as HTMLElement;
    expect(clip.contains(panel)).toBe(false);
  });

  it("lists hidden columns below the visible ones, all toggleable", async () => {
    const toggled: string[] = [];
    const { getByLabelText } = render(() => (
      <ColumnMenu
        columns={columns}
        columnKeys={["type", "parent", "tech"]}
        cols={() => ["type", "parent"]}
        onToggle={(k) => toggled.push(k)}
        onMove={() => {}}
      />
    ));
    fireEvent.click(getByLabelText("Columns"));
    fireEvent.click(await screen.findByText("Technical name"));
    expect(toggled).toEqual(["tech"]);
  });
});
