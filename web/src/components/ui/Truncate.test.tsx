import { describe, it, expect } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import { For } from "solid-js";
import Truncate from "./Truncate";
import { tipOf } from "../../test-tip";
import Button from "../Button";

describe("Truncate and a titled Button (#2486)", () => {
  it("shows the full text of a truncated value on hover, and is not a tab stop", async () => {
    render(() => <Truncate text="a-very-long-value-that-gets-cut" />);
    const el = screen.getByText("a-very-long-value-that-gets-cut");
    expect(el.className).toContain("truncate");
    expect(el.hasAttribute("tabindex")).toBe(false);
    expect(await tipOf(el)).toBe("a-very-long-value-that-gets-cut");
  });

  it("a list with truncated cells has one tab stop per row, not per cell", () => {
    const rows = ["alpha", "beta", "gamma"];
    const { container } = render(() => (
      <table>
        <tbody>
          <For each={rows}>
            {(r) => (
              <tr>
                <td><button type="button">{r}</button></td>
                <td><Truncate text={`${r}-a-long-identity-value`} /></td>
                <td><Truncate as="code" text={`${r}-a-long-rule-expression`} /></td>
              </tr>
            )}
          </For>
        </tbody>
      </table>
    ));
    const stops = container.querySelectorAll("button, a[href], [tabindex]:not([tabindex='-1'])");
    expect(stops.length).toBe(rows.length);
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
