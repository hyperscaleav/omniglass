import { describe, it, expect } from "vitest";
import { For, createSignal } from "solid-js";
import { render } from "@solidjs/testing-library";
import { bindSelectValue } from "./selectvalue";

// A stand-in for every blade that pairs a stored value with an option list the
// server answers for. The options arrive through a signal, so a test drives the
// gap between "the value is known" and "the options exist" by hand, with no
// timer: the whole defect lives in that gap and a sleep would only make it
// probabilistic again.
function mountSelect(value: () => string, options: () => string[]) {
  const { container } = render(() => (
    <select ref={bindSelectValue(value, options)}>
      <option value="" disabled>
        Choose...
      </option>
      <For each={options()}>{(o) => <option value={o}>{o.toUpperCase()}</option>}</For>
    </select>
  ));
  return container.querySelector("select")!;
}

describe("bindSelectValue", () => {
  it("takes the stored value when the options land after the control is bound", () => {
    const [options, setOptions] = createSignal<string[]>([]);
    // "b", never "a": a value that is not the first option is the only kind that
    // can tell a correct binding from the browser's first-option fallback.
    const select = mountSelect(() => "b", options);
    expect(select.value).toBe("");

    setOptions(["a", "b", "c"]);

    expect(select.value).toBe("b");
    expect(select.selectedOptions[0]?.textContent).toBe("B");
  });

  it("applies the value on the first run when the options are already present", () => {
    // The effect must run after <For> has inserted the options, not before, or
    // the no-gap case would fall back too.
    const [options] = createSignal(["a", "b", "c"]);
    expect(mountSelect(() => "b", options).value).toBe("b");
  });

  it("follows the value when it changes with the options unchanged", () => {
    const [options] = createSignal(["a", "b", "c"]);
    const [value, setValue] = createSignal("b");
    const select = mountSelect(value, options);
    expect(select.value).toBe("b");

    setValue("c");

    expect(select.value).toBe("c");
  });

  it("re-applies the value when the options are replaced wholesale", () => {
    // A refetch hands back fresh rows, so <For> (keyed by reference) rebuilds
    // every <option> and the control loses its selection. Nothing about the
    // value changed, so a value= binding would not re-run.
    const [options, setOptions] = createSignal(["a", "b", "c"]);
    const select = mountSelect(() => "b", options);
    expect(select.value).toBe("b");

    setOptions(["a", "b", "c"].slice());

    expect(select.value).toBe("b");
  });

  it("tracks every option source it is given", () => {
    // A control whose options come from two collections (a target picker over
    // both the property and the metric catalog) must re-apply when either lands.
    const [properties, setProperties] = createSignal<string[]>([]);
    const [metrics, setMetrics] = createSignal<string[]>([]);
    const { container } = render(() => (
      <select ref={bindSelectValue(() => "metric:rtt", properties, metrics)}>
        <option value="">None</option>
        <For each={properties()}>{(p) => <option value={`property:${p}`}>{p}</option>}</For>
        <For each={metrics()}>{(m) => <option value={`metric:${m}`}>{m}</option>}</For>
      </select>
    ));
    const select = container.querySelector("select")!;
    setProperties(["serial"]);
    expect(select.value).toBe("");

    setMetrics(["rtt"]);

    expect(select.value).toBe("metric:rtt");
  });

  it("stops applying the value once its owner is disposed", () => {
    // The binding hangs off the element's own ref, so it must die with the
    // branch that rendered it rather than outliving the closed edit face.
    const [options, setOptions] = createSignal<string[]>(["a", "b"]);
    const [value, setValue] = createSignal("b");
    const { container, unmount } = render(() => (
      <select ref={bindSelectValue(value, options)}>
        <option value="" disabled>
          Choose...
        </option>
        <For each={options()}>{(o) => <option value={o}>{o}</option>}</For>
      </select>
    ));
    const select = container.querySelector("select")!;
    expect(select.value).toBe("b");

    unmount();
    setValue("a");
    setOptions(["a", "b", "c"]);

    expect(select.value).toBe("b");
  });

  it("leaves a plain value binding failing the same gap", () => {
    // The defect itself, pinned. A <select> assigned a value it has no option
    // for keeps nothing, and when the options arrive the browser selects the
    // first one instead. Solid re-runs a value= binding only when the value
    // changes, and it did not. If this ever starts passing, the platform
    // changed and the primitive is no longer load-bearing.
    const [options, setOptions] = createSignal<string[]>([]);
    const { container } = render(() => (
      <select value="b">
        <option value="" disabled>
          Choose...
        </option>
        <For each={options()}>{(o) => <option value={o}>{o}</option>}</For>
      </select>
    ));
    const select = container.querySelector("select")!;

    setOptions(["a", "b", "c"]);

    expect(select.value).toBe("a");
  });
});
