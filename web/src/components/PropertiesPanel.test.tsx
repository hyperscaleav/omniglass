import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import type { JSX } from "solid-js";
import PropertiesPanel, { propertyResolutionBlade, propertyBladeId } from "./PropertiesPanel";
import { effectivePropertiesKey, type EffectiveProperty } from "../lib/component_properties";
import { createEditSlot, type BladeEdit } from "../lib/blades";
import { ME_KEY, type Me } from "../lib/auth";

// The panel resolves a component's properties against its product's contract and
// keeps what the component declares off-contract in its own group. Rows are seeded
// into the query cache so no server is needed; the set / clear writes are faked
// where a test drives a Save.
const seed: EffectiveProperty[] = [
  // On contract, not overridden: the contract default is what applies.
  { property_name: "display.diagonal_in", display_name: "Diagonal inches", data_type: "int", required: false, is_set: false, from_contract: true, default_value: 55, value: 55 },
  // On contract, overridden: the component's value wins over the default.
  { property_name: "display.resolution", display_name: "Resolution", data_type: "string", required: false, is_set: true, from_contract: true, default_value: "1920x1080", set_value: "3840x2160", value: "3840x2160", value_id: "v-res" },
  // On contract and required: always overridden, and it must carry a value.
  { property_name: "net.hostname", display_name: "Hostname", data_type: "string", required: true, is_set: true, from_contract: true, set_value: "disp-1.hq", value: "disp-1.hq", value_id: "v-host" },
  // Off contract: set directly on this component, declared by no product.
  { property_name: "mount.height_cm", display_name: "Mount height", data_type: "int", required: false, is_set: true, from_contract: false, set_value: 210, value: 210, value_id: "v-mount" },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(rows: EffectiveProperty[] = seed, edit?: BladeEdit) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...effectivePropertiesKey("disp-1")], rows);
  qc.setQueryData([...ME_KEY], owner);
  return render(() => (
    <QueryClientProvider client={qc}>
      <PropertiesPanel component="disp-1" edit={edit} onOpen={() => {}} />
    </QueryClientProvider>
  ));
}

// The read row is the label's own row; the edit cell wraps the label row and the
// input below it.
const readRow = (label: HTMLElement) => label.closest("div") as HTMLElement;
const editCell = (label: HTMLElement) => (label.closest("div") as HTMLElement).parentElement as HTMLElement;

// Every write the panel made, in order, so a test asserts the API calls rather than
// the panel's internals.
type Call = { method: string; url: string; body: string };
function captureWrites(): Call[] {
  const calls: Call[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    let body = "";
    try { body = await req.clone().text(); } catch { body = ""; }
    calls.push({ method: req.method, url: req.url, body });
    if (req.url.includes("/auth/me")) return json(owner);
    if (req.method === "DELETE") return new Response(null, { status: 204 });
    if (req.method === "PUT") return json({ component: "disp-1", property_name: "x", value: null, value_id: "v" });
    return json({ component: "disp-1", properties: seed });
  });
  return calls;
}

describe("PropertiesPanel", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the contract default on a property the component has not overridden", () => {
    const { getByText } = mount();
    const row = readRow(getByText("Diagonal inches"));
    expect(within(row).getByText("55").className).toContain("text-base-content/70"); // inherited, muted
    expect(within(row).queryByLabelText("override")).toBeNull(); // no dot: the default applies
  });

  it("shows the component's own value, with the override dot, on an overridden property", () => {
    const { getByText, queryByText } = mount();
    const row = readRow(getByText("Resolution"));
    expect(within(row).getByLabelText("override")).toBeTruthy(); // the accent dot
    expect(within(row).getByText("3840x2160").className).toContain("text-primary");
    expect(queryByText("1920x1080")).toBeNull(); // the shadowed default is not in the scan row
  });

  it("groups an off-contract property apart and says what makes it off contract", () => {
    const { getByRole, getByText } = mount();
    const offContract = getByRole("group", { name: /off contract/i });
    expect(within(offContract).getByText("Mount height")).toBeTruthy();
    expect(within(offContract).queryByText("Diagonal inches")).toBeNull(); // contract rows stay above
    expect(getByText(/not declared by its product/i)).toBeTruthy();
  });

  it("explains where properties come from when there is no contract and nothing set", () => {
    const { getByText } = mount([]);
    expect(getByText(/declared by the product it is an instance of/i)).toBeTruthy();
    expect(getByText(/no product contract/i)).toBeTruthy();
  });

  it("blocks the Save when a required property is left empty, before writing anything", async () => {
    const calls = captureWrites();
    const edit = createEditSlot();
    const { getByDisplayValue, getByText } = mount(seed, edit);
    edit.begin();

    // Hostname is required, so it is locked overridden; emptying it fails the flush.
    fireEvent.input(getByDisplayValue("disp-1.hq"), { target: { value: "" } });
    await expect(edit.save()).rejects.toThrow(/required/i);

    expect(getByText("This value is required")).toBeTruthy();
    expect(calls.filter((c) => c.method === "PUT" || c.method === "DELETE")).toEqual([]);
  });

  it("sets what changed and clears what was toggled off on Save", async () => {
    const calls = captureWrites();
    const edit = createEditSlot();
    const { getByText } = mount(seed, edit);
    edit.begin();

    // Override the inherited contract default with a value of this component's own.
    const diagonal = editCell(getByText("Diagonal inches"));
    fireEvent.click(within(diagonal).getByRole("checkbox")); // Override on
    fireEvent.input(within(diagonal).getByRole("spinbutton"), { target: { value: "65" } });

    // Drop the override on Resolution: it falls back to the contract default.
    const resolution = editCell(getByText("Resolution"));
    fireEvent.click(within(resolution).getByRole("checkbox")); // Override off

    await edit.save();

    const puts = calls.filter((c) => c.method === "PUT");
    expect(puts.length).toBe(1); // untouched rows are not rewritten
    expect(puts[0].url).toContain("/components/disp-1/properties/display.diagonal_in");
    expect(JSON.parse(puts[0].body)).toEqual({ value: 65 }); // coerced to its data_type

    const deletes = calls.filter((c) => c.method === "DELETE");
    expect(deletes.length).toBe(1);
    expect(deletes[0].url).toContain("/components/disp-1/properties/display.resolution");
  });

  // A required property whose contract carries a default is already satisfied by
  // that default, so it must stay inheritable. Forcing the override on would pin a
  // redundant copy of the default onto the component on the next Save, and the
  // component would silently stop following the product when the default changed.
  it("leaves a required property that has a contract default inheriting, and writes nothing for it", async () => {
    const requiredWithDefault: EffectiveProperty[] = [
      { property_name: "net.domain", display_name: "Domain", data_type: "string", required: true, is_set: false, from_contract: true, default_value: "hq.example", value: "hq.example" },
    ];
    const calls = captureWrites();
    const edit = createEditSlot();
    const { getByText } = mount(requiredWithDefault, edit);
    edit.begin();

    // The override switch is off (the default applies) and is operable, unlike a
    // required property with nothing to inherit.
    const domain = editCell(getByText("Domain"));
    expect((within(domain).getByRole("checkbox") as HTMLInputElement).checked).toBe(false);

    await edit.save(); // the default satisfies required, so the Save commits

    expect(calls.filter((c) => c.method === "PUT" || c.method === "DELETE")).toEqual([]);
  });
});

// The drill-in re-resolves the property from the blade id (component + property
// name) and renders the deepest-wins chain, which is where the contract and the
// component's own value are told apart step by step.
const Body = propertyResolutionBlade.Body;

function withCache(ui: () => JSX.Element) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...effectivePropertiesKey("disp-1")], seed);
  return render(() => <QueryClientProvider client={qc}>{ui()}</QueryClientProvider>);
}

const chainRow = (badge: HTMLElement) => badge.closest("div") as HTMLElement;

describe("propertyResolutionBlade", () => {
  it("shadows the contract default once the component overrides it", () => {
    const { getByText } = withCache(() => <Body id={propertyBladeId("disp-1", "display.resolution")} />);
    const def = getByText("contract default");
    expect(def.className).toContain("badge-ghost");
    expect(chainRow(def).querySelector(".line-through")).toBeTruthy();
    expect(chainRow(def).textContent).toContain("1920x1080");

    const comp = getByText("this component");
    expect(comp.className).toContain("badge-primary");
    expect(chainRow(comp).textContent).toContain("3840x2160");
  });

  it("makes the contract default the winner when the component has not overridden", () => {
    const { getByText } = withCache(() => <Body id={propertyBladeId("disp-1", "display.diagonal_in")} />);
    expect(getByText("contract default").className).toContain("badge-primary");
    expect(getByText("not set")).toBeTruthy();
    expect(getByText("on contract")).toBeTruthy();
  });

  it("an off-contract property has no contract step to fall back to", () => {
    const { getByText } = withCache(() => <Body id={propertyBladeId("disp-1", "mount.height_cm")} />);
    expect(getByText("off contract")).toBeTruthy();
    expect(getByText(/does not declare this property/i)).toBeTruthy();
    expect(getByText(/clearing it removes the value/i)).toBeTruthy();
  });
});
