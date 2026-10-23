import { describe, it, expect, afterEach, vi } from "vitest";
import { render, waitFor, screen } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { EditorView } from "@codemirror/view";
import { currentCompletions, startCompletion } from "@codemirror/autocomplete";
import { undo } from "@codemirror/commands";
import ExpressionField from "./ExpressionField";
import FieldRow from "./FieldRow";
import { expressionText, typeExpression } from "./ExpressionField.testing";

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
    diagnostics: bad ? [{ line: 2, column: 3, phase: "type", severity: "error", message: "unknown name humidity", unbound: "humidity" }] : [],
    bindings, builtins: ["abs", "len"], budget: { iterations: 10000, memory: 10000, deadline: "5ms" },
  };
}

function mount(initial: string, key = "temperature-c", fail = false, extra: Partial<Parameters<typeof ExpressionField>[0]> = {}) {
  const calls: { source: string; key?: string }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const body = await (input as Request).clone().json();
    calls.push(body);
    if (fail) return new Response(JSON.stringify({ detail: "down" }), { status: 500, headers: { "Content-Type": "application/json" } });
    return json(answer(body.source));
  });
  const [v, setV] = createSignal(initial);
  const checks: (boolean | undefined)[] = [];
  const r = render(() => (
    <FieldRow label="Fires when">
      <ExpressionField site="criteria" sampleKey={key} value={v()} onInput={setV} label="Fire expression" debounce={10} onCheck={(ok) => checks.push(ok)} {...extra} />
    </FieldRow>
  ));
  return { calls, checks, v, setV, unmount: r.unmount };
}

describe("ExpressionField", () => {
  afterEach(() => vi.restoreAllMocks());

  // The editor's accessible name is its visible label (the FieldRow's), so
  // what an author sees is what a voice or screen reader user says.
  const editor = () => screen.findByRole("textbox", { name: "Fires when" });

  it("is a labelled monospace code editor the author types into", async () => {
    const { v, setV } = mount("value > 40");
    const ed = await editor();
    expect(ed.getAttribute("aria-multiline")).toBe("true");
    expect(ed.hasAttribute("aria-label")).toBe(false);
    expect(expressionText(ed)).toBe("value > 40");
    expect(ed.closest(".font-data")).toBeTruthy();
    for (const attr of ["required", "pattern", "minlength", "maxlength"]) expect(ed.hasAttribute(attr)).toBe(false);
    typeExpression(ed, "value > 41");
    // The form's value follows the editor, and the editor follows the form's.
    expect(v()).toBe("value > 41");
    setV("value > 42");
    expect(expressionText(ed)).toBe("value > 42");
  });

  it("takes a value set from outside without moving it into the author's undo history", async () => {
    const { setV } = mount("value > 40");
    const ed = await editor();
    setV("value > 50");
    const view = EditorView.findFromDOM(ed)!;
    expect(undo(view)).toBe(false);
    expect(expressionText(ed)).toBe("value > 50");
  });

  it("is named by its own label where there is no FieldRow", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => json(answer("value")));
    render(() => <ExpressionField site="criteria" sampleKey="k" value="" onInput={() => {}} label="Fire expression" debounce={10} />);
    expect(await screen.findByRole("textbox", { name: "Fire expression" })).toBeTruthy();
  });

  it("keeps the latest check's verdict when an earlier answer lands after it", async () => {
    const resolvers: ((r: Response) => void)[] = [];
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const body = await (input as Request).clone().json();
      if (!body.source) return json(answer(""));
      return new Promise<Response>((res) => resolvers.push(() => res(json(answer(body.source)))));
    });
    const [v, setV] = createSignal("humidity > 1");
    const checks: (boolean | undefined)[] = [];
    render(() => <ExpressionField site="criteria" sampleKey="k" value={v()} onInput={setV} label="Fire expression" debounce={5} onCheck={(ok) => checks.push(ok)} />);
    await waitFor(() => expect(resolvers.length).toBe(1));
    setV("value > 1");
    await waitFor(() => expect(resolvers.length).toBe(2));
    resolvers[1](new Response());
    await screen.findByTestId("expression-ok");
    resolvers[0](new Response());
    await new Promise((r) => setTimeout(r, 20));
    expect(checks.at(-1)).toBe(true);
    expect(screen.queryByTestId("expression-diagnostic")).toBeNull();
  });

  it("says nothing more once it is gone", async () => {
    let release: () => void = () => {};
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const body = await (input as Request).clone().json();
      if (!body.source) return json(answer(""));
      return new Promise<Response>((res) => (release = () => res(json(answer(body.source)))));
    });
    const checks: (boolean | undefined)[] = [];
    const r = render(() => <ExpressionField site="criteria" sampleKey="k" value="value > 1" onInput={() => {}} label="Fire expression" debounce={5} onCheck={(ok) => checks.push(ok)} />);
    await waitFor(() => expect(checks).toEqual([undefined]));
    await new Promise((res) => setTimeout(res, 20));
    r.unmount();
    release();
    await new Promise((res) => setTimeout(res, 20));
    expect(checks).toEqual([undefined]);
  });

  it("offers to bind a name nothing binds, beside the fault", async () => {
    const bound: string[] = [];
    mount("value > 40 &&\n  humidity > 2", "temperature-c", false, { onBind: (name: string) => bound.push(name) });
    const action = await screen.findByRole("button", { name: "Bind humidity to a key" });
    action.click();
    expect(bound).toEqual(["humidity"]);
  });

  it("inserts a template at the cursor, the cursor left in its slot", async () => {
    const templates = [{ label: "max(▮)", text: "max()", cursor: 4, title: "The largest" }];
    mount("", "temperature-c", false, { templates });
    const ed = await editor();
    (await screen.findByRole("button", { name: "max(▮)" })).click();
    const view = EditorView.findFromDOM(ed)!;
    expect(expressionText(ed)).toBe("max()");
    expect(view.state.selection.main.head).toBe(4);
    view.dispatch({ selection: { anchor: 5 } });
    (await screen.findByRole("button", { name: "max(▮)" })).click();
    expect(expressionText(ed)).toBe("max()max()");
    expect(view.state.selection.main.head).toBe(9);
  });

  it("follows a placeholder that changes", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => json(answer("")));
    const [ph, setPh] = createSignal("first");
    render(() => <ExpressionField site="criteria" sampleKey="k" value="" onInput={() => {}} label="Fire expression" placeholder={ph()} debounce={10} />);
    const ed = await screen.findByRole("textbox", { name: "Fire expression" });
    await waitFor(() => expect(ed.querySelector(".cm-placeholder")?.textContent).toBe("first"));
    setPh("second");
    await waitFor(() => expect(ed.querySelector(".cm-placeholder")?.textContent).toBe("second"));
  });

  it("highlights Expr: a binding, a keyword, a string and a number each read as their kind", async () => {
    mount('value > 40 and component.name != "x"');
    const ed = await editor();
    const styled = (text: string) => [...ed.querySelectorAll("span")].find((s) => s.textContent === text)?.className ?? "";
    // Each kind gets its own class, and no two of these share one.
    const kinds = ["value", "and", '"x"', "40"].map(styled);
    expect(kinds.every((k) => k !== "")).toBe(true);
    expect(new Set(kinds).size).toBe(4);
  });

  it("checks the source as it is typed and points at the fault under its line and column", async () => {
    const { calls, checks } = mount("value > 40");
    await waitFor(() => expect(calls.filter((c) => c.source).length).toBe(1));
    typeExpression(await editor(), "value > 40 &&\n  humidity > 2");
    const diag = await screen.findByTestId("expression-diagnostic");
    expect(diag.textContent).toContain("Line 2, column 3");
    expect(diag.textContent).toContain("unknown name humidity");
    const pre = diag.querySelector("pre")!;
    expect(pre.textContent).toBe("  humidity > 2\n  ^");
    // The source echo and its caret are for the eye; the message says it.
    expect(pre.getAttribute("aria-hidden")).toBe("true");
    expect((await editor()).getAttribute("aria-invalid")).toBe("true");
    // The fault is underlined in the source itself, on the word it names.
    const ed = await editor();
    await waitFor(() => expect(ed.querySelector(".cm-lintRange-error")?.textContent).toBe("humidity"));
    expect(calls.at(-1)).toEqual({ site: "criteria", source: "value > 40 &&\n  humidity > 2", key: "temperature-c" });
    // Pending (undefined) while each check is in flight, then its verdict.
    expect(checks).toEqual([undefined, true, undefined, false]);
    typeExpression(await editor(), "value > 40");
    await screen.findByTestId("expression-ok");
    expect((await editor()).querySelector(".cm-lintRange-error")).toBeNull();
    expect((await editor()).hasAttribute("aria-invalid")).toBe(false);
  });

  it("completes exactly the site's bindings and builtins, from the server's answer", async () => {
    mount("value > 40 && ");
    await screen.findByTestId("expression-bindings");
    const view = EditorView.findFromDOM(await editor())!;
    view.dispatch({ selection: { anchor: view.state.doc.length } });
    startCompletion(view);
    await waitFor(() => expect(currentCompletions(view.state).length).toBeGreaterThan(0));
    expect(currentCompletions(view.state).map((c) => c.label).sort()).toEqual(["abs", "component.name", "len", "value"]);
  });

  it("is focused by a click on its FieldRow label, as the textarea was", async () => {
    mount("value > 40");
    const ed = await editor();
    (screen.getByText("Fires when") as HTMLLabelElement).click();
    expect(document.activeElement).toBe(ed);
  });

  it("leaves Tab to move focus on, so the field never traps the keyboard", async () => {
    mount("value > 40");
    const ev = new KeyboardEvent("keydown", { key: "Tab", bubbles: true, cancelable: true });
    (await editor()).dispatchEvent(ev);
    expect(ev.defaultPrevented).toBe(false);
  });

  it("lists the site's bindings and functions from the same answer, each name with its doc", async () => {
    mount("value > 40");
    const list = await screen.findByTestId("expression-bindings");
    expect(list.textContent).toContain("value");
    expect(list.textContent).toContain("component.name");
    expect(screen.getByRole("button", { name: "More about component.name" })).toBeTruthy();
    expect(screen.getByTestId("expression-builtins").textContent).toContain("abs");
  });

  it("asks for the site's vocabulary while the source is still empty, so the first word completes", async () => {
    const { calls, checks } = mount("");
    const list = await screen.findByTestId("expression-bindings");
    expect(list.textContent).toContain("component.name");
    expect(calls).toEqual([{ site: "criteria", source: "", key: "temperature-c" }]);
    // An empty source is never ok, and fetching the vocabulary does not say otherwise.
    expect(checks).toEqual([false]);
    expect(screen.queryByTestId("expression-diagnostic")).toBeNull();
  });

  it("asks for the vocabulary as soon as the key is known, even while the author is already typing", async () => {
    const { calls } = mount("");
    // Typed before the field's debounce would have fired: the source check
    // waits for a pause, the vocabulary does not.
    typeExpression(await editor(), "v");
    await screen.findByTestId("expression-bindings");
    expect(calls.some((c) => c.source === "")).toBe(true);
  });

  it("drops a vocabulary the server stops answering, so it never offers a refused name", async () => {
    let refuse = false;
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      refuse
        ? new Response(JSON.stringify({ detail: "a calc rule reads 1 to 8 inputs, not 0" }), { status: 422, headers: { "Content-Type": "application/json" } })
        : json(answer("value")),
    );
    const [key, setKey] = createSignal("temperature-c");
    render(() => <ExpressionField site="criteria" sampleKey={key()} value="" onInput={() => {}} label="Fire expression" debounce={10} />);
    await screen.findByTestId("expression-bindings");
    refuse = true;
    setKey("humidity-pct");
    await waitFor(() => expect(screen.queryByTestId("expression-bindings")).toBeNull());
  });

  it("checks nothing until the rule reads a key", async () => {
    const { calls } = mount("value > 40", "");
    await new Promise((r) => setTimeout(r, 40));
    expect(calls.length).toBe(0);
    expect(screen.getByText(/Choose what the rule reads/)).toBeTruthy();
  });
});

describe("ExpressionField's verdict", () => {
  afterEach(() => vi.restoreAllMocks());

  it("is not ok for an empty source, a missing key, or a check that failed", async () => {
    const empty = mount("");
    expect(empty.checks).toEqual([false]);
    vi.restoreAllMocks();
    const nokey = mount("value > 1", "");
    expect(nokey.checks).toEqual([false]);
    vi.restoreAllMocks();
    const failed = mount("value > 1", "temperature-c", true);
    await waitFor(() => expect(failed.checks.at(-1)).toBe(false));
  });

  it("clears a failure when the source is emptied", async () => {
    const m = mount("value > 1", "temperature-c", true);
    await waitFor(() => expect(m.checks.at(-1)).toBe(false));
    expect(document.querySelector(".text-error")).toBeTruthy();
    m.setV("");
    await waitFor(() => expect(document.querySelector(".text-error")).toBeNull());
  });
});
