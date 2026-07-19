import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import { fieldResolutionBlade, fieldBladeId } from "./EffectiveFields";
import { effectiveFieldsKey, type EffectiveField } from "../lib/fields";

// The field resolution blade re-resolves a component's effective fields from the
// blade id (component + field name) and renders the deepest-wins chain. These
// tests seed the query cache directly (the TagAdder pattern) so no network runs.
const fields: EffectiveField[] = [
  // Overridden: this component wins, the type default is shadowed.
  { field_id: "f1", name: "gain", display_name: "Gain", data_type: "int", value: -6, set_value: -6, default_value: 0, is_set: true, required: false, value_id: "v1" },
  // Unset with a type default: the default wins.
  { field_id: "f2", name: "phantom", data_type: "bool", value: false, default_value: false, is_set: false, required: false },
  // Unset with no type default: the default step shows a dash.
  { field_id: "f3", name: "serial", data_type: "string", value: null, is_set: false, required: false },
];

const Body = fieldResolutionBlade.Body;
const Title = fieldResolutionBlade.Title;

function withCache(ui: () => JSX.Element) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...effectiveFieldsKey("codec")], fields);
  return render(() => <QueryClientProvider client={qc}>{ui()}</QueryClientProvider>);
}

const row = (badge: HTMLElement) => badge.closest("div") as HTMLElement;

describe("fieldResolutionBlade", () => {
  it("meta line carries the raw key and the data type", () => {
    const { getByText } = withCache(() => <Body id={fieldBladeId("codec", "gain")} />);
    expect(getByText("gain")).toBeTruthy(); // the raw key lives in the drill-in, not the row
    expect(getByText("int")).toBeTruthy();
  });

  it("an override makes this component the winner and strikes the type default", () => {
    const { getByText } = withCache(() => <Body id={fieldBladeId("codec", "gain")} />);
    const def = getByText("type default");
    expect(def.className).toContain("badge-ghost");
    expect(def.className).not.toContain("badge-primary");
    expect(row(def).querySelector(".line-through")).toBeTruthy(); // default shadowed
    expect(row(def).textContent).toContain("0");
    expect(row(def).querySelector("svg")).toBeNull(); // no winner check on the default

    const comp = getByText("this component");
    expect(comp.className).toContain("badge-primary");
    expect(row(comp).textContent).toContain("-6");
    expect(row(comp).querySelector("svg")).toBeTruthy(); // winner check
  });

  it("an unset field with a default makes the type default the winner", () => {
    const { getByText } = withCache(() => <Body id={fieldBladeId("codec", "phantom")} />);
    const def = getByText("type default");
    expect(def.className).toContain("badge-primary");
    expect(row(def).querySelector("svg")).toBeTruthy(); // default wins
    expect(row(def).textContent).toContain("false");
    expect(getByText("not set")).toBeTruthy(); // this component did not override
  });

  it("an unset field with no default shows a dash for the type default", () => {
    const { getByText } = withCache(() => <Body id={fieldBladeId("codec", "serial")} />);
    expect(row(getByText("type default")).textContent).toContain("—");
  });

  it("the title is the display name, falling back to the raw key", () => {
    const named = withCache(() => <Title id={fieldBladeId("codec", "gain")} />);
    expect(named.getByText("Gain")).toBeTruthy();
    named.unmount();

    const unnamed = withCache(() => <Title id={fieldBladeId("codec", "phantom")} />);
    expect(unnamed.getByText("phantom")).toBeTruthy();
  });

  it("a field no longer on the type shows the absent-field fallback", () => {
    const { getByText } = withCache(() => <Body id={fieldBladeId("codec", "ghost")} />);
    expect(getByText(/no longer declared/i)).toBeTruthy();
  });
});
