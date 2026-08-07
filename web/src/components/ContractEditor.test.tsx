import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ContractEditor from "./ContractEditor";
import { classifierPropertiesKey, type ClassifierKind, type ClassifierProperty } from "../lib/classifier_properties";
import { classifierMetricsKey, type ClassifierMetric } from "../lib/classifier_metrics";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY, type MetricRow } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";
import { BladeEditContext, createEditSlot } from "../lib/blades";

// One editor curates the declared-properties contract of all three classifiers: a
// product (for its components), a standard (for the systems conforming to it), and
// a location type (for the locations of that type). These cover the two arcs the
// product editor's own test does not, plus the seam itself: the kind picks the API
// route, the authorization resource, and the copy. Data is seeded into the query
// cache so no server is needed; the PUT / DELETE fetches are faked where a test
// drives them.
//
// The mount hosts the editor inside a blade edit slot, as the pages do: the
// controls are edit-state affordances (#621), so the default mount enters edit
// mode; `editing: false` witnesses the read state.
const contract: ClassifierProperty[] = [
  { property_type_name: "seat_count", property_type_id: "seat_count-id", required: true },
  { property_type_name: "display_count", property_type_id: "display_count-id", default_value: 2, required: false },
];

const catalog: PropertyRow[] = [
  { name: "seat_count", data_type: "int", display_name: "Seat count", official: true },
  { name: "display_count", data_type: "int", display_name: "Display count", official: true },
  { name: "has_camera", data_type: "bool", display_name: "Has camera", official: true },
];

const owner: Me = { principal: { id: "p", kind: "human" }, permissions: [">"], grants: [] };
// A reader of everything: it can see a contract but never write one.
const reader: Me = { principal: { id: "r", kind: "human" }, permissions: ["*:read"], grants: [] };

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

function mount(kind: ClassifierKind, opts: { me?: Me; official?: boolean; lines?: ClassifierProperty[]; editing?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...classifierPropertiesKey(kind, "meeting-room")], opts.lines ?? contract);
  qc.setQueryData([...PROPERTIES_KEY], catalog);
  qc.setQueryData([...ME_KEY], opts.me ?? owner);
  const edit = createEditSlot();
  if (opts.editing ?? true) edit.begin();
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <BladeEditContext.Provider value={edit}>
        <ContractEditor classifier={kind} id="meeting-room" official={opts.official ?? false} />
      </BladeEditContext.Provider>
    </QueryClientProvider>
  ));
  return { ...result, edit };
}

describe("ContractEditor on a standard", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each declared property with its default and required marker", () => {
    const { getByText } = mount("standard");
    expect(getByText("Declared properties")).toBeTruthy();
    expect(getByText("the standard contract")).toBeTruthy();
    // The copy names what conforms to a standard: a system, not a component.
    expect(getByText(/A system conforming to this standard inherits/)).toBeTruthy();
    expect(getByText("seat_count")).toBeTruthy();
    expect(getByText("2")).toBeTruthy(); // the declared default
    expect(getByText("no default")).toBeTruthy();
    expect(getByText("required")).toBeTruthy();
    expect(getByText("optional")).toBeTruthy();
  });

  it("declares a picked catalog property, PUTting to the standards contract route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_type_name: "has_camera", property_type_id: "has_camera-id", default_value: true, required: false });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    // Only the properties NOT already declared are offered.
    const picker = getByLabelText("Property to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "has_camera"]);

    fireEvent.change(picker, { target: { value: "has_camera" } });
    fireEvent.input(getByLabelText("Default for the new property"), { target: { value: "true" } });
    fireEvent.click(getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/properties/has_camera");
    expect(await put!.json()).toEqual({ required: false, default_value: true }); // coerced to bool
  });

  it("edits a line in place, PUTting the revised default", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_type_name: "display_count", property_type_id: "display_count-id", default_value: 3, required: false });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    fireEvent.click(getByLabelText("Edit display_count"));
    const input = getByLabelText("Default for display_count") as HTMLInputElement;
    expect(input.value).toBe("2"); // seeded from the declared default
    fireEvent.input(input, { target: { value: "3" } });
    fireEvent.click(getByLabelText("Save display_count"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/standards/meeting-room/properties/display_count");
    expect(await put!.json()).toEqual({ required: false, default_value: 3 });
  });

  it("withdraws a line after confirmation, calling DELETE", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") {
        del = req.clone();
        return new Response(null, { status: 204 });
      }
      return json({ properties: contract });
    });

    const { getByLabelText } = mount("standard");
    fireEvent.click(getByLabelText("Withdraw seat_count"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(confirmSpy.mock.calls[0][0]).toContain("standard's contract"); // the copy names the classifier
    expect(del!.url).toContain("/standards/meeting-room/properties/seat_count");
  });

  it("renders an official standard's contract read-only", () => {
    const { getByText, queryByLabelText } = mount("standard", { official: true });
    expect(getByText("seat_count")).toBeTruthy(); // the list still renders
    expect(getByText("seed-owned, read-only")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit seat_count")).toBeNull();
    expect(queryByLabelText("Withdraw seat_count")).toBeNull();
  });

  it("hides the write controls from a principal without standard:update", () => {
    const { getByText, queryByLabelText } = mount("standard", { me: reader });
    expect(getByText("seat_count")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit seat_count")).toBeNull();
    expect(queryByLabelText("Withdraw seat_count")).toBeNull();
  });

  // The blade model (#621): outside blade edit mode the panel is facts alone,
  // whatever the caller may do; the pencil is the one route to the controls.
  it("renders the contract as facts alone while the blade is in read mode", () => {
    const { getByText, queryByLabelText } = mount("standard", { editing: false });
    expect(getByText("seat_count")).toBeTruthy();
    expect(getByText("2")).toBeTruthy();
    expect(queryByLabelText("Property to declare")).toBeNull();
    expect(queryByLabelText("Edit seat_count")).toBeNull();
    expect(queryByLabelText("Withdraw seat_count")).toBeNull();
  });

  it("shows the empty state when a standard declares nothing", () => {
    const { getByText } = mount("standard", { lines: [] });
    expect(getByText("This standard declares no properties.")).toBeTruthy();
  });
});

describe("ContractEditor on a location type", () => {
  afterEach(() => vi.restoreAllMocks());

  it("speaks location-type language over the location-types route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ property_type_name: "has_camera", property_type_id: "has_camera-id", required: true });
      }
      return json({ properties: contract });
    });

    const { getByText, getByLabelText } = mount("location-type");
    expect(getByText("the location type contract")).toBeTruthy();
    expect(getByText(/A location of this type inherits/)).toBeTruthy();

    fireEvent.change(getByLabelText("Property to declare"), { target: { value: "has_camera" } });
    fireEvent.click(getByLabelText("Declare property"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/location-types/meeting-room/properties/has_camera");
  });

  // A location type's contract is part of the location_type registry, so the
  // server gates it on location_type:update, not location:update. A principal
  // holding only location writes must not see the write controls.
  it("gates its write controls on location_type:update, not location:update", () => {
    const locationWriter: Me = { principal: { id: "l", kind: "human" }, permissions: ["*:read", "location:update", "location:delete"], grants: [] };
    const typeWriter: Me = { principal: { id: "t", kind: "human" }, permissions: ["*:read", "location_type:update", "location_type:delete"], grants: [] };
    expect(mount("location-type", { me: locationWriter }).queryByLabelText("Property to declare")).toBeNull();
    expect(mount("location-type", { me: typeWriter }).getByLabelText("Property to declare")).toBeTruthy();
  });

  it("shows the empty state when a location type declares nothing", () => {
    const { getByText } = mount("location-type", { lines: [] });
    expect(getByText("This location type declares no properties.")).toBeTruthy();
  });
});

// The metric lane: the same editor over the metric contract routes and the
// metric catalog. The lane switches the data layer, the copy, and the picker's
// catalog; the row mechanics (PUT upsert, DELETE withdraw, official read-only)
// are shared with the property lane.
const metricContract: ClassifierMetric[] = [
  { metric_type_name: "icmp-rtt-avg", metric_type_id: "icmp-rtt-avg-id", default_value: 10.5, required: true },
  { metric_type_name: "tcp-open", metric_type_id: "tcp-open-id", required: false },
];

const metricCatalog: MetricRow[] = [
  { name: "icmp-rtt-avg", data_type: "float", display_name: "ICMP RTT (avg)", official: true },
  { name: "tcp-open", data_type: "int", display_name: "TCP Port Open", official: true },
  { name: "tcp-connect-time", data_type: "float", display_name: "TCP Connect Time", official: true },
];

function mountMetric(kind: ClassifierKind, opts: { me?: Me; official?: boolean; lines?: ClassifierMetric[]; editing?: boolean } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...classifierMetricsKey(kind, "meeting-room")], opts.lines ?? metricContract);
  qc.setQueryData([...METRICS_KEY], metricCatalog);
  qc.setQueryData([...ME_KEY], opts.me ?? owner);
  const edit = createEditSlot();
  if (opts.editing ?? true) edit.begin();
  return render(() => (
    <QueryClientProvider client={qc}>
      <BladeEditContext.Provider value={edit}>
        <ContractEditor classifier={kind} lane="metric" id="meeting-room" official={opts.official ?? false} />
      </BladeEditContext.Provider>
    </QueryClientProvider>
  ));
}

describe("ContractEditor's metric lane", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists each declared metric with its default and required marker", () => {
    const { getByText } = mountMetric("product");
    expect(getByText("Declared metrics")).toBeTruthy();
    expect(getByText("the product contract")).toBeTruthy();
    // The copy speaks the metric lane: the value side is the series, not an
    // override typed on a detail page.
    expect(getByText(/A component of this product carries every metric declared here/)).toBeTruthy();
    expect(getByText("icmp-rtt-avg")).toBeTruthy();
    expect(getByText("10.5")).toBeTruthy(); // the declared default
    expect(getByText("no default")).toBeTruthy();
    expect(getByText("required")).toBeTruthy();
    expect(getByText("optional")).toBeTruthy();
  });

  it("declares a picked catalog metric, PUTting to the products metric route", async () => {
    let put: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "PUT") {
        put = req.clone();
        return json({ metric_type_name: "tcp-connect-time", metric_type_id: "tcp-connect-time-id", default_value: 42, required: false });
      }
      return json({ metrics: metricContract });
    });

    const { getByLabelText } = mountMetric("product");
    // Only the metrics NOT already declared are offered.
    const picker = getByLabelText("Metric to declare") as HTMLSelectElement;
    expect(Array.from(picker.options).map((o) => o.value)).toEqual(["", "tcp-connect-time"]);

    fireEvent.change(picker, { target: { value: "tcp-connect-time" } });
    fireEvent.input(getByLabelText("Default for the new metric"), { target: { value: "42" } });
    fireEvent.click(getByLabelText("Declare metric"));

    await waitFor(() => expect(put).toBeTruthy());
    expect(put!.url).toContain("/products/meeting-room/metrics/tcp-connect-time");
    expect(await put!.json()).toEqual({ required: false, default_value: 42 }); // coerced to number
  });

  it("withdraws a line after confirmation, calling DELETE on the standards metric route", async () => {
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    let del: Request | undefined;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "DELETE") {
        del = req.clone();
        return new Response(null, { status: 204 });
      }
      return json({ metrics: metricContract });
    });

    const { getByLabelText } = mountMetric("standard");
    fireEvent.click(getByLabelText("Withdraw icmp-rtt-avg"));

    await waitFor(() => expect(del).toBeTruthy());
    expect(confirmSpy.mock.calls[0][0]).toContain("standard's contract"); // the copy names the classifier
    expect(del!.url).toContain("/standards/meeting-room/metrics/icmp-rtt-avg");
  });

  it("renders an official classifier's metric contract read-only", () => {
    const { getByText, queryByLabelText } = mountMetric("product", { official: true });
    expect(getByText("icmp-rtt-avg")).toBeTruthy(); // the list still renders
    expect(getByText("seed-owned, read-only")).toBeTruthy();
    expect(queryByLabelText("Metric to declare")).toBeNull();
    expect(queryByLabelText("Edit icmp-rtt-avg")).toBeNull();
    expect(queryByLabelText("Withdraw icmp-rtt-avg")).toBeNull();
  });

  it("shows the metric empty state when a location type declares nothing", () => {
    const { getByText } = mountMetric("location-type", { lines: [] });
    expect(getByText("This location type declares no metrics.")).toBeTruthy();
  });

  // The blade model (#621) holds on this lane too: read mode is facts alone.
  it("renders the metric contract as facts alone while the blade is in read mode", () => {
    const { getByText, queryByLabelText } = mountMetric("product", { editing: false });
    expect(getByText("icmp-rtt-avg")).toBeTruthy();
    expect(getByText("10.5")).toBeTruthy();
    expect(queryByLabelText("Metric to declare")).toBeNull();
    expect(queryByLabelText("Edit icmp-rtt-avg")).toBeNull();
    expect(queryByLabelText("Withdraw icmp-rtt-avg")).toBeNull();
  });
});

// Leaving blade edit mode discards the panel's open drafts (#621's review):
// a draft typed before Cancel must not resurrect pre-filled on the next
// pencil press, where a reflexive confirm click would PUT it.
describe("ContractEditor drafts die with the edit session", () => {
  afterEach(() => vi.restoreAllMocks());

  it("closes an open row edit and clears the add draft when edit mode ends", async () => {
    const { edit, getByLabelText, queryByLabelText } = mount("standard");
    fireEvent.click(getByLabelText("Edit seat_count"));
    const input = getByLabelText("Default for seat_count") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "999" } });
    const picker = getByLabelText("Property to declare") as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: "has_camera" } });
    edit.cancel();
    await Promise.resolve();
    expect(queryByLabelText("Default for seat_count")).toBeNull();
    edit.begin();
    await Promise.resolve();
    expect(queryByLabelText("Default for seat_count")).toBeNull();
    expect((getByLabelText("Property to declare") as HTMLSelectElement).value).toBe("");
  });
});
