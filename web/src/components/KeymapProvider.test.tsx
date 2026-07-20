import { describe, it, expect } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import { onCleanup, onMount } from "solid-js";
import { KeymapProvider, useKeymap, type BindingSpec } from "./KeymapProvider";

// A tiny consumer that registers one scope and records which actions fired.
function Consumer(props: { name: string; priority: number; bindings: BindingSpec[]; active?: () => boolean }) {
  const km = useKeymap();
  onMount(() => {
    const off = km.register({ name: props.name, priority: props.priority, active: props.active, bindings: () => props.bindings });
    onCleanup(off);
  });
  return null;
}

const keys = () => ({ command_palette: "mod+k", close_blade: "Escape", open_detail: "d" });

describe("KeymapProvider", () => {
  it("dispatches a window keydown to the matching binding", () => {
    const fired: string[] = [];
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Consumer name="global" priority={10} bindings={[{ action: "palette", label: "Palette", combo: "mod+k", run: () => fired.push("palette") }]} />
      </KeymapProvider>
    ));
    fireEvent.keyDown(window, { key: "k", ctrlKey: true });
    expect(fired).toEqual(["palette"]);
  });

  it("runs the higher-priority scope when two scopes claim the same chord", () => {
    const fired: string[] = [];
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Consumer name="list" priority={20} bindings={[{ action: "list-d", label: "d", combo: "d", run: () => fired.push("list") }]} />
        <Consumer name="blade" priority={30} bindings={[{ action: "blade-d", label: "d", combo: "d", run: () => fired.push("blade") }]} />
      </KeymapProvider>
    ));
    fireEvent.keyDown(window, { key: "d" });
    expect(fired).toEqual(["blade"]);
  });

  it("does not dispatch to an inactive scope", () => {
    const fired: string[] = [];
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Consumer name="blade" priority={30} active={() => false} bindings={[{ action: "close", label: "Close", combo: "Escape", run: () => fired.push("close") }]} />
      </KeymapProvider>
    ));
    fireEvent.keyDown(window, { key: "Escape" });
    expect(fired).toEqual([]);
  });

  it("suppresses a bare-key binding while typing but still fires a modified combo", () => {
    const fired: string[] = [];
    const { container } = render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <input data-testid="field" />
        <Consumer name="list" priority={20} bindings={[{ action: "d", label: "d", combo: "d", run: () => fired.push("d") }]} />
        <Consumer name="global" priority={10} bindings={[{ action: "palette", label: "Palette", combo: "mod+k", run: () => fired.push("palette") }]} />
      </KeymapProvider>
    ));
    const field = container.querySelector("input")!;
    fireEvent.keyDown(field, { key: "d" });
    expect(fired).toEqual([]); // bare key suppressed in a text field
    fireEvent.keyDown(field, { key: "k", ctrlKey: true });
    expect(fired).toEqual(["palette"]); // modified combo still fires while typing
  });

  it("exposes the active scopes for the help overlay, ordered by priority", () => {
    let ctl: ReturnType<typeof useKeymap> | undefined;
    function Probe() {
      ctl = useKeymap();
      return null;
    }
    render(() => (
      <KeymapProvider keys={keys} platform="Win32">
        <Probe />
        <Consumer name="global" priority={10} bindings={[{ action: "palette", label: "Palette", combo: "mod+k", run: () => {} }]} />
        <Consumer name="blade" priority={30} bindings={[{ action: "close", label: "Close", combo: "Escape", run: () => {} }]} />
      </KeymapProvider>
    ));
    const names = ctl!.activeScopes().map((s) => s.name);
    expect(names).toEqual(["blade", "global"]);
  });
});
