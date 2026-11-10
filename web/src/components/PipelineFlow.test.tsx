import "@testing-library/jest-dom/vitest";
import { describe, it, expect } from "vitest";
import { fireEvent, render, screen, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import PipelineFlow from "./PipelineFlow";
import type { PlatformPipeline, PlatformPipelineHop } from "../lib/platformhealth";
import { uuidFor } from "../lib/testids";

// Where work is lost (#1646): the hops the view returns, in its order, each a
// focusable button carrying in, out and the loss ratio for the chosen kind;
// selecting one opens its explainer (keys, catalog descriptions, the class
// that records it, and each component's share linking to its series).
const fig = (inn: number, out: number) => ({ in: inn, out, ...(inn > 0 ? { loss: (inn - out) / inn } : {}) });
const span = (inn: number, out: number) => ({ newest: fig(inn, out), last_hour: fig(inn, out) });
const KEYS = ["delivery-in-count", "delivery-out-count", "delivery-loss-ratio"];

const nodeHop: PlatformPipelineHop = {
  id: "node",
  label: "Node",
  description: "The node's spool, from a run's output to the stream.",
  component_class: "node",
  keys: KEYS,
  kinds: [
    { kind: "metric", ...span(100, 99) },
    { kind: "log", ...span(0, 0) },
  ],
  total: span(100, 99),
  by_component: [
    { component: uuidFor("alpha"), name: "edge-alpha", kinds: [{ kind: "metric", ...span(2, 1) }], total: span(2, 1) },
    { component: uuidFor("bravo"), name: "edge-bravo", kinds: [{ kind: "metric", ...span(98, 98) }, { kind: "log", ...span(0, 0) }], total: span(98, 98) },
  ],
};

const ingestHop: PlatformPipelineHop = {
  id: "ingest",
  label: "Ingest",
  description: "The server's ingest, from the stream to the store.",
  component_class: "server",
  keys: KEYS,
  kinds: [
    { kind: "metric", ...span(99, 99) },
    { kind: "event", ...span(4, 4) },
  ],
  total: span(103, 103),
  by_component: [{ component: uuidFor("server"), name: "lab-server", kinds: [{ kind: "metric", ...span(99, 99) }, { kind: "event", ...span(4, 4) }], total: span(103, 103) }],
};

const pipeline: PlatformPipeline = {
  keys: [
    { name: "delivery-in-count", label: "Delivery In", unit: "", description: "Items the hop took in over the report window." },
    { name: "delivery-out-count", label: "Delivery Out", unit: "", description: "Items the hop handed on over the report window." },
    { name: "delivery-loss-ratio", label: "Delivery Loss", unit: "", description: "The share of what went in that did not come out." },
  ],
  hops: [nodeHop, ingestHop],
  end_to_end: [
    { kind: "metric", newest: 0.01, last_hour: 0.01 },
    { kind: "event", newest: 0, last_hour: 0 },
  ],
  end_to_end_total: { newest: 0.0097, last_hour: 0.0097 },
};

function mount(p: PlatformPipeline) {
  return render(() => (
    <Router>
      <Route path="*" component={() => <PipelineFlow pipeline={p} />} />
    </Router>
  ));
}

const hopButtons = () => within(screen.getByRole("list", { name: "Pipeline hops" })).getAllByRole("button");

describe("PipelineFlow", () => {
  it("draws the hops in the payload's order, never an order of its own", () => {
    const first = mount({ ...pipeline, hops: [ingestHop, nodeHop] });
    expect(hopButtons().map((b) => b.getAttribute("data-hop"))).toEqual(["ingest", "node"]);
    first.unmount();
    mount(pipeline);
    expect(hopButtons().map((b) => b.getAttribute("data-hop"))).toEqual(["node", "ingest"]);
  });

  it("puts in and out beside each hop's ratio, every kind summed by default", () => {
    mount(pipeline);
    const node = screen.getByTestId("pipeline-hop-node");
    expect(within(node).getByTestId("hop-in")).toHaveTextContent("100");
    expect(within(node).getByTestId("hop-out")).toHaveTextContent("99");
    expect(within(node).getByTestId("hop-loss")).toHaveTextContent("1%");
    expect(within(screen.getByTestId("pipeline-hop-ingest")).getByTestId("hop-loss")).toHaveTextContent("0%");
  });

  it("tints a ratio above 0 and leaves a 0 untinted", () => {
    mount(pipeline);
    const lossy = within(screen.getByTestId("pipeline-hop-node")).getByTestId("hop-loss");
    const clean = within(screen.getByTestId("pipeline-hop-ingest")).getByTestId("hop-loss");
    expect(lossy).toHaveAttribute("data-tinted", "true");
    expect(lossy.className).toContain("text-error");
    expect(clean).toHaveAttribute("data-tinted", "false");
    expect(clean.className).not.toContain("text-error");
  });

  it("filters every hop's figures and the end to end by the chosen kind", () => {
    mount(pipeline);
    const selector = screen.getByRole("group", { name: "Kind" });
    expect(within(selector).getAllByRole("button").map((b) => b.textContent)).toEqual(["All", "Metric", "Event", "Log"]);
    fireEvent.click(within(selector).getByRole("button", { name: "Event" }));
    expect(within(selector).getByRole("button", { name: "Event" })).toHaveAttribute("aria-pressed", "true");
    // The node hop carried no events: absent, not 0.
    const node = screen.getByTestId("pipeline-hop-node");
    expect(within(node).getByTestId("hop-loss")).toHaveTextContent("not reported");
    expect(within(node).queryByTestId("hop-in")).toBeNull();
    expect(within(screen.getByTestId("pipeline-hop-ingest")).getByTestId("hop-in")).toHaveTextContent("4");
    expect(screen.getByTestId("pipeline-end-to-end")).toHaveTextContent("0%");
    fireEvent.click(within(selector).getByRole("button", { name: "Metric" }));
    expect(within(screen.getByTestId("pipeline-hop-ingest")).getByTestId("hop-in")).toHaveTextContent("99");
    expect(screen.getByTestId("pipeline-end-to-end")).toHaveTextContent("1%");
  });

  it("reads a kind with nothing in as no ratio, never a fake 0", () => {
    mount(pipeline);
    fireEvent.click(screen.getByRole("button", { name: "Log" }));
    const loss = within(screen.getByTestId("pipeline-hop-node")).getByTestId("hop-loss");
    expect(loss).toHaveTextContent("nothing in");
    expect(loss).not.toHaveTextContent("0%");
    expect(within(screen.getByTestId("pipeline-hop-node")).getByTestId("hop-in")).toHaveTextContent("0");
  });

  it("opens a hop's explainer: its keys with their catalog descriptions, the class that records it, and each component's share linking to its series", () => {
    mount(pipeline);
    expect(screen.queryByTestId("pipeline-hop-detail")).toBeNull();
    fireEvent.click(screen.getByTestId("pipeline-hop-node"));
    expect(screen.getByTestId("pipeline-hop-node")).toHaveAttribute("aria-pressed", "true");
    const detail = screen.getByTestId("pipeline-hop-detail");
    expect(detail).toHaveTextContent("The node's spool, from a run's output to the stream.");
    expect(detail).toHaveTextContent(/every node's platform component/);
    for (const k of KEYS) expect(within(detail).getByText(k)).toBeInTheDocument();
    expect(detail).toHaveTextContent("The share of what went in that did not come out.");
    const alpha = within(detail).getByTestId("pipeline-component-edge-alpha");
    expect(within(alpha).getByRole("link", { name: "edge-alpha" })).toHaveAttribute("href", `/components/${uuidFor("alpha")}`);
    expect(within(alpha).getByTestId("hop-loss")).toHaveTextContent("50%");
    expect(within(detail).getByTestId("pipeline-component-edge-bravo")).toBeInTheDocument();
  });

  it("filters the explainer's component rows by the chosen kind too", () => {
    mount(pipeline);
    fireEvent.click(screen.getByTestId("pipeline-hop-node"));
    fireEvent.click(screen.getByRole("button", { name: "Log" }));
    const detail = screen.getByTestId("pipeline-hop-detail");
    expect(within(within(detail).getByTestId("pipeline-component-edge-alpha")).getByTestId("hop-loss")).toHaveTextContent("not reported");
    expect(within(within(detail).getByTestId("pipeline-component-edge-bravo")).getByTestId("hop-loss")).toHaveTextContent("nothing in");
  });

  it("makes every hop a keyboard-reachable button that selects on activation", () => {
    mount(pipeline);
    for (const b of hopButtons()) {
      expect(b.tagName).toBe("BUTTON");
      expect(b).not.toHaveAttribute("tabindex", "-1");
    }
    const ingest = screen.getByTestId("pipeline-hop-ingest");
    ingest.focus();
    expect(document.activeElement).toBe(ingest);
    fireEvent.click(ingest);
    expect(screen.getByTestId("pipeline-hop-detail")).toHaveTextContent("The server's ingest");
    fireEvent.click(ingest);
    expect(screen.queryByTestId("pipeline-hop-detail")).toBeNull();
  });
});
