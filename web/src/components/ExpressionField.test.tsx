import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, waitFor, screen } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import ExpressionField from "./ExpressionField";
import FieldRow from "./FieldRow";

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

const bindings = [
  { name: "value", type: "float64", doc: "The sample's value" },
  { name: "component.name", type: "string", doc: "The component's name" },
];

// The server's answer for a source: a type error at 2:3 for one naming
// humidity, clean otherwise.
function answer(source: string) {
  const bad = source.includes("humidity");
  return {
    ok: !bad, site: "criteria", value_type: "number", result_type: "bool",
    diagnostics: bad ? [{ line: 2, column: 3, phase: "type", message: "unknown name humidity" }] : [],
    bindings, builtins: ["abs", "len"], budget: { iterations: 10000, memory: 10000, deadline: "5ms" },
  };
}

function mount(initial: string, key = "temperature-c") {
  const calls: { source: string; key?: string }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const body = await (input as Request).clone().json();
    calls.push(body);
    return json(answer(body.source));
  });
  const [v, setV] = createSignal(initial);
  const checks: boolean[] = [];
  render(() => (
    <FieldRow label="Fires when">
      <ExpressionField site="criteria" sampleKey={key} value={v()} onInput={setV} label="Fires when" debounce={10} onCheck={(ok) => checks.push(ok)} />
    </FieldRow>
  ));
  return { calls, checks, setV };
}

describe("ExpressionField", () => {
  afterEach(() => vi.restoreAllMocks());

  it("is a labelled monospace textarea the author types into", () => {
    mount("value > 40");
    const area = screen.getByLabelText("Fires when") as HTMLTextAreaElement;
    expect(area.tagName).toBe("TEXTAREA");
    expect(area.value).toBe("value > 40");
    expect(area.className).toContain("font-data");
    for (const attr of ["required", "pattern", "minlength", "maxlength"]) expect(area.hasAttribute(attr)).toBe(false);
  });

  it("checks the source as it is typed and points at the fault under its line and column", async () => {
    const { calls, checks } = mount("value > 40");
    await waitFor(() => expect(calls.length).toBe(1));
    fireEvent.input(screen.getByLabelText("Fires when"), { target: { value: "value > 40 &&\n  humidity > 2" } });
    const diag = await screen.findByTestId("expression-diagnostic");
    expect(diag.textContent).toContain("Line 2, column 3");
    expect(diag.textContent).toContain("unknown name humidity");
    const pre = diag.querySelector("pre")!;
    expect(pre.textContent).toBe("  humidity > 2\n  ^");
    expect(screen.getByLabelText("Fires when").getAttribute("aria-invalid")).toBe("true");
    expect(calls.at(-1)).toEqual({ site: "criteria", source: "value > 40 &&\n  humidity > 2", key: "temperature-c" });
    expect(checks).toEqual([true, false]);
  });

  it("lists the site's bindings and functions from the same answer, each name with its doc", async () => {
    mount("value > 40");
    const list = await screen.findByTestId("expression-bindings");
    expect(list.textContent).toContain("value");
    expect(list.textContent).toContain("component.name");
    expect(screen.getByRole("button", { name: "More about component.name" })).toBeTruthy();
    expect(screen.getByTestId("expression-builtins").textContent).toContain("abs");
  });

  it("checks nothing until the rule reads a key", async () => {
    const { calls } = mount("value > 40", "");
    await new Promise((r) => setTimeout(r, 40));
    expect(calls.length).toBe(0);
    expect(screen.getByText(/Choose what the rule reads/)).toBeTruthy();
  });
});
