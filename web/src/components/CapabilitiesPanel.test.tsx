import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CapabilitiesPanel from "./CapabilitiesPanel";
import { componentCapabilitiesKey } from "../lib/component_capabilities";
import { CAPABILITIES_KEY, type Capability } from "../lib/capabilities";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { ME_KEY, type Me } from "../lib/auth";

// The panel shows what a component provides, resolved: its product's capabilities,
// plus what it adds, minus what it suppresses. The API returns the resolved set
// only, so the three origins are read against the product's declaration. Data is
// seeded into the query cache so no server is needed; the writes are faked where a
// test drives one.
//
// The product declares touch-panel and speaker; the component resolves to
// touch-panel (inherited) and microphone (its own addition), so speaker is a
// suppression it made.
const resolved = ["touch-panel", "microphone"];
const product: Product = {
  id: "crestron-tsw",
  display_name: "Crestron TSW",
  kind: "device",
  capabilities: ["touch-panel", "speaker"],
  official: true,
};
const catalog: Capability[] = [
  { id: "touch-panel", display_name: "Touch panel", official: true },
  { id: "speaker", display_name: "Speaker", official: true },
  { id: "microphone", display_name: "Microphone", official: true },
  { id: "camera", display_name: "Camera", official: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(opts: { rows?: string[]; canUpdate?: boolean; productId?: string } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...componentCapabilitiesKey("panel-1")], opts.rows ?? resolved);
  qc.setQueryData([...PRODUCTS_KEY], [product]);
  qc.setQueryData([...CAPABILITIES_KEY], catalog);
  qc.setQueryData([...ME_KEY], owner);
  return render(() => (
    <QueryClientProvider client={qc}>
      <CapabilitiesPanel
        component="panel-1"
        productId={"productId" in opts ? opts.productId : product.id}
        canUpdate={opts.canUpdate ?? true}
      />
    </QueryClientProvider>
  ));
}

const capRow = (id: HTMLElement) => id.closest("div.flex") as HTMLElement;

describe("CapabilitiesPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("tells an inherited capability from one declared on the component", () => {
    const { getByText } = mount();
    expect(within(capRow(getByText(/touch-panel/))).getByText("from the product")).toBeTruthy();
    expect(within(capRow(getByText(/microphone/))).getByText("on this component")).toBeTruthy();
  });

  // A suppressed capability is absent from the resolved set, which is what
  // suppression means, so it is still listed (struck through): it is the only way
  // to see what the component is refusing to inherit, and the only way back.
  it("still lists what the component suppresses, struck through", () => {
    const { getByText } = mount();
    const row = capRow(getByText(/speaker/));
    expect(within(row).getByText("suppressed")).toBeTruthy();
    expect(row.querySelector(".line-through")).toBeTruthy();
  });

  it("reads every capability as the component's own when it has no product", () => {
    const { getByText, queryByText } = mount({ productId: undefined });
    expect(within(capRow(getByText(/touch-panel/))).getByText("on this component")).toBeTruthy();
    expect(queryByText("from the product")).toBeNull();
    expect(queryByText("suppressed")).toBeNull(); // nothing declared it, so nothing is being refused
  });

  it("suppresses an inherited capability, declaring it absent on this component", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") { put = req.clone(); return new Response(null, { status: 204 }); }
      return json({ component: "panel-1", capabilities: resolved });
    });

    const { getByLabelText } = mount();
    fireEvent.click(getByLabelText("Suppress touch-panel"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/components/panel-1/capabilities/touch-panel");
    expect(await put!.json()).toEqual({ present: false });
  });

  it("clears the component's own fact, falling back to the product", async () => {
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") { del = req.clone(); return new Response(null, { status: 204 }); }
      return json({ component: "panel-1", capabilities: resolved });
    });

    const { getByLabelText } = mount();
    // The suppression is a fact of the component's, so clearing it restores what
    // the product declares.
    fireEvent.click(getByLabelText("Clear speaker"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(del!.url).toContain("/components/panel-1/capabilities/speaker");
  });

  it("adds a capability the product does not declare", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") { put = req.clone(); return new Response(null, { status: 204 }); }
      return json({ component: "panel-1", capabilities: resolved });
    });

    const { getByLabelText } = mount();
    // Only what is not already on the component is offered, suppressions included:
    // restoring one of those is a clear, not an add.
    const picker = getByLabelText("Capability to add") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "camera"]);

    fireEvent.change(picker, { target: { value: "camera" } });
    fireEvent.click(getByLabelText("Add capability"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/components/panel-1/capabilities/camera");
    expect(await put!.json()).toEqual({ present: true });
  });

  it("shows no write control when the caller cannot update the component", () => {
    const { getByText, queryByLabelText } = mount({ canUpdate: false });
    expect(getByText(/touch-panel/)).toBeTruthy(); // the read still renders
    expect(queryByLabelText("Suppress touch-panel")).toBeNull();
    expect(queryByLabelText("Clear speaker")).toBeNull();
    expect(queryByLabelText("Capability to add")).toBeNull();
  });

  it("says a component with nothing resolved and no product declaration provides nothing", () => {
    const { getByText } = mount({ rows: [], productId: undefined });
    expect(getByText(/provides nothing yet/i)).toBeTruthy();
  });
});
