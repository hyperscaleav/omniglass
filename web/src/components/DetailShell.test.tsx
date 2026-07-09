import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { RelatedList, type RelatedItem } from "./DetailShell";

const items: RelatedItem[] = [
  { id: "u-1", kind: "user", name: "alice", badge: "human" },
  { id: "u-2", kind: "user", name: "ingest-bot", badge: "service" },
];

describe("RelatedList", () => {
  it("renders a row per item and drills on click when onOpen is present", () => {
    const onOpen = vi.fn();
    render(() => <RelatedList label="Members" items={items} empty="none" onOpen={onOpen} />);
    expect(screen.getByText("alice")).toBeTruthy();
    expect(screen.getByText("ingest-bot")).toBeTruthy();
    fireEvent.click(screen.getByText("alice"));
    expect(onOpen).toHaveBeenCalledTimes(1);
    expect(onOpen.mock.calls[0][0].id).toBe("u-1");
  });

  it("shows the empty text when there are no items", () => {
    render(() => <RelatedList label="Members" items={[]} empty="No members yet." />);
    expect(screen.getByText("No members yet.")).toBeTruthy();
  });

  it("fires onRemove from the row's remove button", () => {
    const onRemove = vi.fn();
    render(() => <RelatedList label="Members" items={items} empty="none" onRemove={onRemove} />);
    const removes = screen.getAllByLabelText("Remove");
    fireEvent.click(removes[0]);
    expect(onRemove).toHaveBeenCalledTimes(1);
    expect(onRemove.mock.calls[0][0].id).toBe("u-1");
  });

  it("adds via the picker when add is present", () => {
    const onAdd = vi.fn();
    render(() => (
      <RelatedList
        label="Members"
        items={[]}
        empty="none"
        add={{ placeholder: "Add a member...", options: [{ id: "u-9", label: "carol" }], onAdd, canAdd: true }}
      />
    ));
    const select = screen.getByRole("combobox") as HTMLSelectElement;
    fireEvent.change(select, { target: { value: "u-9" } });
    fireEvent.click(screen.getByText("Add"));
    expect(onAdd).toHaveBeenCalledWith("u-9");
  });
});
