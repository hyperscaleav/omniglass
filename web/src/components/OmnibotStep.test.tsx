import { describe, it, expect, afterEach } from "vitest";
import { render, screen, cleanup, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import OmnibotStep, { STEP_RENDERERS } from "./OmnibotStep";
import type { Step } from "../lib/assistant";

// A step card is one tool call omnibot made (#1167): the tool, what it was
// asked, and what came back. A tool with a renderer shows its result the way
// the console shows that thing elsewhere; any other tool, or a result its
// renderer cannot read, falls back to the generic card, so a new tool is never
// a blank.

afterEach(cleanup);

function show(step: Step) {
  return render(() => (
    <Router>
      <Route path="*" component={() => <OmnibotStep step={step} />} />
    </Router>
  ));
}

const step = (tool: string, args: Record<string, unknown>, result?: unknown): Step => ({
  kind: "step",
  id: "c1",
  tool,
  args,
  result: result === undefined ? undefined : typeof result === "string" ? result : JSON.stringify(result),
});

describe("the generic step card", () => {
  it("names the tool, lists its arguments, and keeps the raw result to hand", () => {
    show(step("list-alarms", { component: "amp-9", limit: 5 }, { alarms: [] }));
    const card = screen.getByTestId("omnibot-step");
    expect(within(card).getByText("list-alarms")).toBeTruthy();
    expect(within(card).getByText("component")).toBeTruthy();
    expect(within(card).getByText("amp-9")).toBeTruthy();
    expect(within(card).getByText("5")).toBeTruthy();
    expect(within(card).getByTestId("omnibot-step-raw").textContent).toContain('"alarms": []');
  });

  it("says it is running until a result arrives", () => {
    show(step("list-alarms", {}));
    expect(screen.getByText("Running")).toBeTruthy();
  });

  it("marks a declined call and shows no result", () => {
    show({ ...step("delete-component", { name: "amp-9" }), decision: "declined" });
    expect(screen.getByText("Declined")).toBeTruthy();
    expect(screen.queryByText("Running")).toBeNull();
  });

  it("marks an approved call", () => {
    show({ ...step("create-component", { name: "amp-9" }, { name: "amp-9" }), decision: "approved" });
    expect(screen.getByText("Approved")).toBeTruthy();
  });
});

describe("the tool renderers", () => {
  it("registers the component reads and reachability", () => {
    expect(Object.keys(STEP_RENDERERS).sort()).toEqual(["get-component", "get-component-reachability", "list-components"]);
  });

  it("shows a component read as a link to its page", () => {
    show(step("get-component", { name: "amp-9" }, { id: "u-amp", name: "amp-9", label: "Lobby amp", product: "xpa-2" }));
    const link = screen.getByRole("link", { name: "Lobby amp" });
    expect(link.getAttribute("href")).toBe("/components/u-amp");
    expect(link.parentElement?.textContent).toContain("amp-9");
  });

  it("lists components as links", () => {
    show(step("list-components", {}, { components: [{ id: "u-1", name: "amp-1" }, { id: "u-2", name: "amp-2", label: "Amp two" }] }));
    expect(screen.getByRole("link", { name: "amp-1" }).getAttribute("href")).toBe("/components/u-1");
    expect(screen.getByRole("link", { name: "Amp two" }).getAttribute("href")).toBe("/components/u-2");
  });

  it("says when a list came back empty", () => {
    show(step("list-components", {}, { components: [] }));
    expect(screen.getByText("No components.")).toBeTruthy();
  });

  it("shows reachability as each endpoint's ladder", () => {
    show(
      step("get-component-reachability", { name: "amp-9" }, {
        component: "amp-9",
        endpoints: [
          { endpoint: "ctl", transport: "tcp", address: "10.0.0.9", verdict: { value: "up", ts: "2026-11-17T00:00:00Z" }, layers: [], history: [] },
        ],
      }),
    );
    expect(screen.getByText("ctl")).toBeTruthy();
    expect(screen.getAllByTestId("ladder-rung").length).toBeGreaterThan(0);
  });

  it("falls back to the generic card when the result is not what the renderer reads", () => {
    show(step("get-component", { name: "amp-9" }, "not found"));
    expect(screen.queryByRole("link")).toBeNull();
    expect(screen.getByTestId("omnibot-step-raw").textContent).toContain("not found");
  });
});
