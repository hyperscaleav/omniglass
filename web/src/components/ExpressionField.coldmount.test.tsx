import { describe, it, expect, beforeAll, afterEach, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import ExpressionField from "./ExpressionField";
import FieldRow from "./FieldRow";
import { warmExpressionEditor } from "./ExpressionField.testing";

// The editor's chunk loaded cold (#1068): a loaded full run spends longer
// transforming CodeMirror than a test's default 1s wait, and until it mounts
// the FieldRow's label names a non-labellable host div. The mock stands in
// for that cold load; warmExpressionEditor pays it before any test, so a
// test's wait covers only the mount.
const COLD_LOAD_MS = 1500;
vi.mock("./expressionEditorView", async (actual) => {
  await new Promise((r) => setTimeout(r, COLD_LOAD_MS));
  return actual();
});

beforeAll(warmExpressionEditor);

describe("ExpressionField whose editor chunk loads slowly", () => {
  afterEach(() => vi.restoreAllMocks());

  it("is found by its label within the default wait once the chunk is warm", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      async () => new Response(JSON.stringify({ ok: true, diagnostics: [], bindings: [], builtins: [] }), { status: 200, headers: { "Content-Type": "application/json" } }),
    );
    render(() => (
      <FieldRow label="Fires when">
        <ExpressionField site="criteria" sampleKey="k" value="value > 1" onInput={() => {}} label="Fires when" />
      </FieldRow>
    ));
    const area = await screen.findByLabelText("Fires when");
    expect(area.getAttribute("role")).toBe("textbox");
  });
});
