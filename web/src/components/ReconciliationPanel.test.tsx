import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ReconciliationPanel from "./ReconciliationPanel";
import { ownerPropertiesKey, type EffectiveProperty, type PropertyOwnerKind } from "../lib/owner_properties";

// The panel reads the owner's properties read (#1297), the same cache entry the
// Properties panel fills, so the two never disagree. Rows are seeded into the
// query cache so no server is needed. Drift is the server's flag, rendered as-is.
const row = (p: Partial<EffectiveProperty> & { property_type_name: string }): EffectiveProperty => ({
  property_type_id: `${p.property_type_name}-id`,
  data_type: "string",
  required: false,
  is_set: false,
  from_contract: false,
  ...p,
});

const seed: EffectiveProperty[] = [
  // Declared, and a live endpoint disagrees: drift, naming the instance.
  row({
    property_type_name: "firmware-version", label: "Firmware version", is_set: true, set_value: "1.0.0", value: "1.0.0",
    reconciliation: { want: "1.0.0", told: null, is: "2.0.0", is_instance: "panel-ip", drift: true },
  }),
  // Observed only: nothing declares it, so there is no want and no drift.
  row({
    property_type_name: "serial-number", label: "Serial number",
    reconciliation: { want: null, told: null, is: "SN-42", is_instance: "", drift: false },
  }),
  // Told only: a command set it.
  row({
    property_type_name: "video-input", label: "Video Input",
    reconciliation: { want: null, told: "dp1", is: null, is_instance: "", drift: false },
  }),
  // Calculated: no reconciliation, so not a row here.
  row({ property_type_name: "busy-flag", label: "Busy", value: true, source_rule: "busy", source_rule_id: "r1" }),
];

function mount(rows: EffectiveProperty[] = seed, kind: PropertyOwnerKind = "component", name = "disp-1") {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ownerPropertiesKey(kind, name)], rows);
  const owner = kind === "system" ? { system: name } : { component: name };
  return render(() => (
    <QueryClientProvider client={qc}>
      <ReconciliationPanel {...owner} />
    </QueryClientProvider>
  ));
}

describe("ReconciliationPanel", () => {
  it("renders a want/told/is row per reconciled property, by its label", () => {
    const { getByText } = mount();
    expect(getByText("Firmware version")).toBeTruthy();
    expect(getByText("1.0.0")).toBeTruthy();
    expect(getByText("2.0.0")).toBeTruthy();
  });

  it("names the instance the observed value came from", () => {
    const { getByText } = mount();
    expect(getByText(/panel-ip/)).toBeTruthy();
  });

  it("lists a property that is only observed, with nothing wanted", () => {
    const { getByText } = mount();
    expect(getByText("Serial number")).toBeTruthy();
    expect(getByText("SN-42")).toBeTruthy();
  });

  it("lists a property a command told, with the told value", () => {
    const { getByText } = mount();
    expect(getByText("Video Input")).toBeTruthy();
    expect(getByText("dp1")).toBeTruthy();
  });

  it("leaves out a calculated property, which carries no reconciliation", () => {
    const { queryByText, getByText } = mount();
    expect(queryByText("Busy")).toBeNull();
    expect(getByText(/3 properties/)).toBeTruthy();
  });

  it("marks a drifting property and counts the drift", () => {
    const { getByText, getAllByText } = mount();
    expect(getByText(/1 drifting/)).toBeTruthy();
    expect(getAllByText("drift").length).toBe(1);
  });

  it("reads a system's properties the same way", () => {
    const { getByText } = mount(seed, "system", "huddle-1");
    expect(getByText("SN-42")).toBeTruthy();
  });

  it("shows the empty state when nothing is declared, observed or told", () => {
    const { getByText } = mount([], "component", "c2");
    expect(getByText(/nothing declared, observed or told/i)).toBeTruthy();
  });
});
