import { describe, it, expect, afterEach, vi } from "vitest";
import { caretOf, validateExpression, previewEventRule } from "./expressions";

describe("a diagnostic's position", () => {
  it("points a caret under the column on its line", () => {
    expect(caretOf("value > 1 &&\n  nope > 2", 2, 3)).toEqual({ text: "  nope > 2", caret: "  ^" });
    expect(caretOf("value >", 1, 7)).toEqual({ text: "value >", caret: "      ^" });
  });

  it("has none for a diagnostic without a position, or past the source", () => {
    expect(caretOf("x", 0, 0)).toBeNull();
    expect(caretOf("x", 3, 1)).toBeNull();
  });
});

describe("the expression routes", () => {
  afterEach(() => vi.restoreAllMocks());
  const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

  it("POSTs a source to validate and returns the answer", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(json({ ok: true, diagnostics: [], bindings: [] }));
    const got = await validateExpression({ site: "criteria", source: "value > 1", key: "temperature-c" });
    expect(got.ok).toBe(true);
    const req = spy.mock.calls[0][0] as Request;
    expect(req.url).toMatch(/\/expressions:validate$/);
    expect(await req.json()).toEqual({ site: "criteria", source: "value > 1", key: "temperature-c" });
  });

  it("POSTs a candidate rule to preview", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(json({ in_force_now: [], edges: [], sustains_ignored: true }));
    await previewEventRule({ sample_key: "endpoint-reachable", fire_criteria: { expr: 'value == "down"' } });
    expect((spy.mock.calls[0][0] as Request).url).toMatch(/\/event-rules:preview$/);
  });
});
