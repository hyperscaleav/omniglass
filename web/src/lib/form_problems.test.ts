import { describe, it, expect } from "vitest";
import { createFieldFocus, formProblems, separator } from "./form_problems";

const labels = { name: "Name", sampleKey: "Reads", fireValue: "Fires when it reads" } as const;
const order = ["name", "sampleKey", "fireValue"] as const;

describe("formProblems", () => {
  it("lists each failing field in form order with its label and message", () => {
    expect(formProblems({ fireValue: "A number.", name: "A name." }, order, labels)).toEqual([
      { key: "name", label: "Name", message: "A name." },
      { key: "fireValue", label: "Fires when it reads", message: "A number." },
    ]);
  });
  it("is empty when nothing fails", () => {
    expect(formProblems({}, order, labels)).toEqual([]);
  });
});

describe("separator", () => {
  it("joins a list the way a sentence does", () => {
    expect([0, 1, 2].map((i) => separator(i, 3))).toEqual([", ", " and ", ""]);
    expect([0, 1].map((i) => separator(i, 2))).toEqual([" and ", ""]);
    expect(separator(0, 1)).toBe("");
  });
});

describe("createFieldFocus", () => {
  it("focuses the first registered field that has a problem, and the control inside a wrapper", () => {
    const wrap = document.createElement("div");
    const input = document.createElement("input");
    input.setAttribute("data-field-target", "");
    wrap.appendChild(input);
    const plain = document.createElement("input");
    document.body.append(wrap, plain);
    const f = createFieldFocus();
    f.register("sampleKey")(wrap);
    f.register("name")(plain);
    expect(f.focusFirst(["name", "sampleKey"])).toBe("name");
    expect(document.activeElement).toBe(plain);
    expect(f.focusFirst(["sampleKey"])).toBe("sampleKey");
    expect(document.activeElement).toBe(input);
    expect(f.focusFirst(["unknown"])).toBeUndefined();
    const lone = document.createElement("textarea");
    document.body.append(lone);
    f.registerFinder("expr", () => lone);
    expect(f.focusFirst(["expr"])).toBe("expr");
    expect(document.activeElement).toBe(lone);
  });
});
