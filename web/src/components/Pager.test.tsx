import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi } from "vitest";
import { fireEvent, render, screen } from "@solidjs/testing-library";
import Pager from "./Pager";

describe("Pager", () => {
  it("reads the range and steps to the neighbours", () => {
    const onPage = vi.fn();
    render(() => <Pager total={201} page={1} size={25} onPage={onPage} />);
    expect(screen.getByTestId("pager-range")).toHaveTextContent("26-50 of 201");
    fireEvent.click(screen.getByRole("button", { name: "Next page" }));
    expect(onPage).toHaveBeenLastCalledWith(2);
    fireEvent.click(screen.getByRole("button", { name: "Previous page" }));
    expect(onPage).toHaveBeenLastCalledWith(0);
  });
  it("disables the step that has nowhere to go and clamps the last range", () => {
    render(() => <Pager total={201} page={8} size={25} onPage={() => {}} />);
    expect(screen.getByTestId("pager-range")).toHaveTextContent("201-201 of 201");
    expect(screen.getByRole("button", { name: "Next page" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Previous page" })).toBeEnabled();
  });
  it("shows the count and no steps for one page, and 0 of 0 for an empty list", () => {
    const { unmount } = render(() => <Pager total={3} page={0} size={25} onPage={() => {}} />);
    expect(screen.getByTestId("pager-range")).toHaveTextContent("1-3 of 3");
    expect(screen.queryByRole("button")).toBeNull();
    unmount();
    render(() => <Pager total={0} page={0} size={25} onPage={() => {}} />);
    expect(screen.getByTestId("pager-range")).toHaveTextContent("0 of 0");
  });
});
