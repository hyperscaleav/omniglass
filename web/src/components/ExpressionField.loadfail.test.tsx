import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import ExpressionField from "./ExpressionField";

// The editor is a chunk of its own (#1000). A tab still holding an old
// build's chunk names cannot load it: the field says so and is never ok,
// rather than standing as an empty frame a form could save past.
vi.mock("./expressionEditorView", () => {
  throw new Error("Failed to fetch dynamically imported module");
});

describe("ExpressionField when its editor cannot load", () => {
  afterEach(() => vi.restoreAllMocks());

  it("says so and refuses the source", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify({ ok: true, diagnostics: [], bindings: [], builtins: [] }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    const checks: (boolean | undefined)[] = [];
    render(() => <ExpressionField site="criteria" sampleKey="k" value="value > 1" onInput={() => {}} label="Fire expression" debounce={5} onCheck={(ok) => checks.push(ok)} />);
    expect(await screen.findByText(/editor did not load/)).toBeTruthy();
    await new Promise((r) => setTimeout(r, 30));
    expect(checks.at(-1)).toBe(false);
  });
});
