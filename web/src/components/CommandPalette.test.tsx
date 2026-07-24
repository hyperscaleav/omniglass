import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import CommandPalette from "./CommandPalette";

// The palette needs a Router for useNavigate; Route path="*" renders it under any
// path. The Dialog portals to the body, so screen queries reach it.
function mount(props: { onShowHelp?: () => void; onClose?: () => void }) {
  return render(() => (
    <Router>
      <Route path="*" component={() => <CommandPalette open onClose={props.onClose ?? (() => {})} onShowHelp={props.onShowHelp} />} />
    </Router>
  ));
}

describe("CommandPalette", () => {
  it("offers the keyboard-help action with its ? hint and runs it on click", () => {
    const onShowHelp = vi.fn();
    const onClose = vi.fn();
    mount({ onShowHelp, onClose });

    const row = screen.getByText("Keyboard shortcuts");
    expect(row).toBeTruthy();
    // The shortcut-hint column shows the ? key.
    expect(screen.getByText("?")).toBeTruthy();

    fireEvent.click(row);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(onShowHelp).toHaveBeenCalledTimes(1);
  });

  it("omits the help action when no handler is provided", () => {
    render(() => (
      <Router>
        <Route path="*" component={() => <CommandPalette open onClose={() => {}} />} />
      </Router>
    ));
    expect(screen.queryByText("Keyboard shortcuts")).toBeNull();
  });
});
