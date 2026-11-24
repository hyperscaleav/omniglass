import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import Truncate from "./Truncate";
import Button from "../Button";

describe("Truncate and a titled Button (#2486)", () => {
  it("shows the full text of a truncated value on keyboard focus", async () => {
    render(() => <Truncate text="a-very-long-value-that-gets-cut" />);
    const el = screen.getByText("a-very-long-value-that-gets-cut");
    expect(el.className).toContain("truncate");
    fireEvent.keyDown(document.body, { key: "Tab" });
    el.focus();
    expect((await screen.findByRole("tooltip")).textContent).toBe("a-very-long-value-that-gets-cut");
  });

  it("a titled icon button shows its title on focus and has no title attribute", async () => {
    render(() => <Button square label="Edit thing" title="Edit" />);
    const b = screen.getByRole("button", { name: "Edit thing" });
    expect(b.hasAttribute("title")).toBe(false);
    fireEvent.keyDown(document.body, { key: "Tab" });
    b.focus();
    expect((await screen.findByRole("tooltip")).textContent).toBe("Edit");
  });

  it("a disabled titled button stays focusable and says why", async () => {
    render(() => <Button disabled title="Not allowed yet">Go</Button>);
    const wrap = screen.getByRole("button", { name: "Go" });
    expect(wrap.getAttribute("aria-disabled")).toBe("true");
    fireEvent.keyDown(document.body, { key: "Tab" });
    wrap.focus();
    expect((await screen.findByRole("tooltip")).textContent).toBe("Not allowed yet");
  });
});
