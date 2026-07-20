import { describe, it, expect } from "vitest";
import { render, screen, fireEvent } from "@solidjs/testing-library";
import { onCleanup, onMount } from "solid-js";
import { KeymapProvider, useKeymap, type BindingSpec } from "./KeymapProvider";
import KeyboardHelp from "./KeyboardHelp";

function Consumer(props: { name: string; priority: number; bindings: BindingSpec[] }) {
  const km = useKeymap();
  onMount(() => {
    const off = km.register({ name: props.name, priority: props.priority, bindings: () => props.bindings });
    onCleanup(off);
  });
  return null;
}

const keys = () => ({ command_palette: "mod+k" });

describe("KeyboardHelp", () => {
  it("lists the live registry grouped by scope with platform-native combos", () => {
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Consumer name="global" priority={10} bindings={[{ action: "palette", label: "Command palette", combo: "mod+k", run: () => {} }]} />
        <Consumer name="blade" priority={30} bindings={[{ action: "close", label: "Close blade", combo: "Escape", run: () => {} }]} />
        <KeyboardHelp open onClose={() => {}} />
      </KeymapProvider>
    ));
    // Section headings for the two active scopes.
    expect(screen.getByText("Global")).toBeTruthy();
    expect(screen.getByText("Blade")).toBeTruthy();
    // Labels and platform-formatted combos.
    expect(screen.getByText("Command palette")).toBeTruthy();
    expect(screen.getByText("Ctrl+K")).toBeTruthy();
    expect(screen.getByText("Close blade")).toBeTruthy();
    expect(screen.getByText("Esc")).toBeTruthy();
  });

  it("the All view lists every catalogued shortcut, even ones no active scope binds", () => {
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Consumer name="global" priority={10} bindings={[{ action: "palette", label: "Command palette", combo: "mod+k", run: () => {} }]} />
        <KeyboardHelp open onClose={() => {}} />
      </KeymapProvider>
    ));
    // Help is in the catalog but not registered by any active scope here.
    expect(screen.queryByText("Show keyboard shortcuts")).toBeNull();
    fireEvent.click(screen.getByText("All"));
    // Now every catalogued action shows, with its label from the catalog doc.
    expect(screen.getByText("Show keyboard shortcuts")).toBeTruthy();
    expect(screen.getByText("Open the detail blade")).toBeTruthy();
  });
});
