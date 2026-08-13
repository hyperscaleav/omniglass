import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Products from "./Products";
import { PRODUCTS_KEY, type Product } from "../lib/products";
import { COMPONENT_TYPES_KEY, type ComponentType } from "../lib/component_types";
import { VENDORS_KEY, type Vendor } from "../lib/vendors";
import { DRIVERS_KEY, type Driver } from "../lib/drivers";
import { classifierPropertiesKey, type ClassifierProperty } from "../lib/classifier_properties";
import { classifierMetricsKey } from "../lib/classifier_metrics";
import { PROPERTIES_KEY, type PropertyRow } from "../lib/properties";
import { METRICS_KEY } from "../lib/metric_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Products is the product catalog on the flat FlatList surface (the model a
// component is an instance of). An official row is read-only, same
// invariant as the Types catalog's official rows: no edit pencil, no Delete.
// Data is seeded into the query cache so no server is needed; the vendor and
// driver registries the pickers read are seeded too, so the create form
// stays network-free.
// Wire truth (api/products.go): vendor/driver/parent arrive as BOTH the kebab
// handle (vendor) and the uuid (vendor_id).
const seed: Product[] = [
  { id: uuidFor("prod-crestron-tsw-1070"), name: "crestron-tsw-1070", display_name: "Crestron TSW-1070", kind: "device", component_type: "touch-panel", component_type_id: uuidFor("ct-touch-panel"), vendor: "crestron", vendor_id: uuidFor("ven-crestron"), driver: "crestron-ct", driver_id: uuidFor("drv-crestron-ct"), official: true },
  { id: uuidFor("prod-acme-panel"), name: "acme-panel", display_name: "Acme Panel", kind: "device", component_type: "display", component_type_id: uuidFor("ct-display"), vendor: "acme-av", vendor_id: uuidFor("ven-acme-av"), official: false },
];

// The seeded component_type tree this page's Type picker renders (a slice of
// internal/seed/component_types.yaml): display is a root with its own icon,
// interactive-display a child that inherits it, touch-panel a sibling root.
// resolved_icon is the server's answer per row (#695), which is what a product
// with no override of its own now falls back to.
const componentTypes: ComponentType[] = [
  { id: uuidFor("ct-display"), name: "display", display_name: "Display", official: true, forked: false, stem: "display", abbrev: "fp", icon: "monitor", resolved_icon: "monitor", default_tags: [] },
  { id: uuidFor("ct-interactive-display"), name: "interactive-display", display_name: "Interactive Display", official: true, forked: false, parent: "display", parent_id: uuidFor("ct-display"), resolved_icon: "monitor", default_tags: [] },
  { id: uuidFor("ct-touch-panel"), name: "touch-panel", display_name: "Touch Panel", official: true, forked: false, stem: "panel", abbrev: "tp", icon: "touchpad", resolved_icon: "touchpad", default_tags: [] },
];

const vendors: Vendor[] = [
  { id: uuidFor("ven-crestron"), name: "crestron", display_name: "Crestron", kind: "manufacturer", official: true },
  { id: uuidFor("ven-acme-av"), name: "acme-av", display_name: "Acme AV", kind: "integrator", official: false },
];
const drivers: Driver[] = [{ id: uuidFor("drv-crestron-ct"), name: "crestron-ct", display_name: "Crestron CT", official: true }];

// The custom product's declared-property contract and the catalog its editor
// joins each line to; the metric lane mounts beside it, kept inert with empty
// seeds.
const contract: ClassifierProperty[] = [{ property_type_name: "serial-number", property_type_id: "serial-number-id", required: true }];
const propertyCatalog: PropertyRow[] = [
  { name: "serial-number", data_type: "string", display_name: "Serial number", official: true },
  { name: "port-count", data_type: "int", display_name: "Port count", official: true },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...PRODUCTS_KEY], seed);
  qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
  qc.setQueryData([...VENDORS_KEY], vendors);
  qc.setQueryData([...DRIVERS_KEY], drivers);
  qc.setQueryData([...PROPERTIES_KEY], propertyCatalog);
  qc.setQueryData([...METRICS_KEY], []);
  qc.setQueryData([...classifierPropertiesKey("product", "acme-panel")], contract);
  qc.setQueryData([...classifierPropertiesKey("product", "crestron-tsw-1070")], []);
  qc.setQueryData([...classifierMetricsKey("product", "acme-panel")], []);
  qc.setQueryData([...classifierMetricsKey("product", "crestron-tsw-1070")], []);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Products />
    </QueryClientProvider>
  ));
}

describe("Products page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists a seeded product, an official row greys edit/delete, and create opens for an admin", async () => {
    mount();
    expect(await screen.findByText("Crestron TSW-1070")).toBeInTheDocument();

    // official row: the pair renders greyed with the official reason
    fireEvent.click(screen.getByText("Crestron TSW-1070"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const deleteBtn = within(blade).getByRole("button", { name: /delete/i }) as HTMLButtonElement;
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");

    // create is available to an admin. The label match is anchored because the
    // Name field's hint mentions the display name too.
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    expect(screen.getByLabelText(/^Name/)).toBeInTheDocument();
  });

  it("a custom (non-official) row carries edit and delete", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("editing a custom row exposes the vendor and driver pickers", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    fireEvent.click(within(blade).getByLabelText("Edit"));
    // vendor + driver selects both offer the seeded registry rows
    expect(within(blade).getByRole("option", { name: "Crestron" })).toBeInTheDocument();
    expect(within(blade).getByRole("option", { name: "Crestron CT" })).toBeInTheDocument();
  });

  // The blade model (#621): a blade opens read-only, and EVERY mutating control,
  // the contract panels' declare picker and per-line edit/withdraw included,
  // appears only after the pencil flips the blade into edit mode.
  it("keeps the contract's declare/edit/withdraw controls out of read mode until the pencil flips edit", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode: the declared line renders as a fact row...
    expect(within(blade).getByText("serial-number")).toBeInTheDocument();
    expect(within(blade).getByText("required")).toBeInTheDocument();
    // ...and nothing shaped like a mutating control renders, even for an admin.
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Metric to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit serial-number")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Withdraw serial-number")).not.toBeInTheDocument();
    // The pencil enters edit mode; the existing controls return as they were.
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByLabelText("Property to declare")).toBeInTheDocument();
    expect(within(blade).getByLabelText("Edit serial-number")).toBeInTheDocument();
    expect(within(blade).getByLabelText("Withdraw serial-number")).toBeInTheDocument();
  });

  // The permissionless arm of the same rule: a viewer's blade is locked, so
  // the pencil renders greyed (naming the missing permission) and there is
  // still no route into the controls.
  it("gives a viewer a greyed pencil and no contract controls on a custom row", async () => {
    mount(viewer);
    fireEvent.click(screen.getByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("serial-number")).toBeInTheDocument();
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Requires product:update");
    expect(within(blade).queryByLabelText("Property to declare")).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Withdraw serial-number")).not.toBeInTheDocument();
  });

  it("hides New product for a caller without product:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New product/i)).toBeNull();
  });
});

// #614: the component_type registry returns as the product's genus (ADR-0085),
// so classifying a product is now a required, pickable fact, not an implicit
// derivation from kind. The picker offers the seeded tree, indented by depth,
// so a nested type (interactive-display under display) reads as nested.
describe("Products type picker (#614)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the seeded component_type tree, a child option after its parent", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    const typeSelect = (await screen.findByLabelText("Type")) as HTMLSelectElement;
    const labels = Array.from(typeSelect.options).map((o) => o.textContent?.trim());
    expect(labels).toEqual(["Display", "Interactive Display", "Touch Panel"]);
    // The child renders indented with a leading non-breaking space run (an
    // ordinary space would collapse in a <select>, ComponentTypeSelect.tsx);
    // the parent and the sibling root carry none. textContent, not .text: the
    // latter strips leading/trailing whitespace per the HTMLOptionElement
    // spec, which would silently defeat this exact assertion.
    const nbsp = String.fromCharCode(160);
    const raw = Array.from(typeSelect.options).map((o) => o.textContent ?? "");
    expect(raw[0].startsWith(nbsp)).toBe(false);
    expect(raw[1].startsWith(nbsp)).toBe(true);
    expect(raw[2].startsWith(nbsp)).toBe(false);
  });

  it("blocks Create product until a type is chosen, even with a display name and a kind", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    const display = screen.getByPlaceholderText("Crestron TSW-1070") as HTMLInputElement;
    fireEvent.input(display, { target: { value: "Acme Screen" } });
    const submit = screen.getByRole("button", { name: /create product/i }) as HTMLButtonElement;
    expect(submit.disabled).toBe(true);
    fireEvent.change(await screen.findByLabelText("Type"), { target: { value: "display" } });
    expect(submit.disabled).toBe(false);
  });

  it("sends the chosen component_type on create", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes("/products")) {
        sent = JSON.parse(await req.clone().text());
        return new Response(JSON.stringify({ ...seed[1], name: "acme-screen", component_type: "display" }), { status: 201, headers: { "Content-Type": "application/json" } });
      }
      return new Response(JSON.stringify({ products: [] }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    fireEvent.input(screen.getByPlaceholderText("Crestron TSW-1070"), { target: { value: "Acme Screen" } });
    fireEvent.change(await screen.findByLabelText("Type"), { target: { value: "display" } });
    fireEvent.click(screen.getByRole("button", { name: /create product/i }));
    await waitFor(() => expect(sent).toBeTruthy());
    expect((sent as { component_type: string }).component_type).toBe("display");
  });
});

// #614: kind narrows to device/app/service; vm folds into app.
describe("Products kind options (#614/ADR-0086)", () => {
  it("offers device, app, and service, and never vm", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    const kindSelect = screen.getByLabelText("Kind") as HTMLSelectElement;
    const values = Array.from(kindSelect.options).map((o) => o.value);
    expect(values).toEqual(["device", "app", "service"]);
  });
});

// #614: the icon lives on the type, a product may override it (ADR-0085's
// "product.icon becomes an override" clause). The preview resolves the type's
// icon until the operator sets one, then resolves the override instead.
describe("Products icon fallback and override (#614)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("previews the classified type's icon on the create form until an override is typed", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    fireEvent.change(await screen.findByLabelText("Type"), { target: { value: "display" } });
    // No override yet: the icon input carries the type's icon as its
    // placeholder, not a value (nothing has been typed).
    const iconInput = screen.getByLabelText("Icon") as HTMLInputElement;
    expect(iconInput.value).toBe("");
    expect(iconInput.placeholder).toBe("monitor");
    // Typing an override changes the placeholder's role: the value now wins.
    fireEvent.input(iconInput, { target: { value: "tv" } });
    expect(iconInput.value).toBe("tv");
  });

  it("shows a custom row's own icon in the blade, not the type's, once one is set", async () => {
    const withOverride: Product = { ...seed[1], icon: "tv" };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...PRODUCTS_KEY], [seed[0], withOverride]);
    qc.setQueryData([...COMPONENT_TYPES_KEY], componentTypes);
    qc.setQueryData([...VENDORS_KEY], vendors);
    qc.setQueryData([...DRIVERS_KEY], drivers);
      qc.setQueryData([...PROPERTIES_KEY], propertyCatalog);
    qc.setQueryData([...METRICS_KEY], []);
    qc.setQueryData([...classifierPropertiesKey("product", "acme-panel")], contract);
    qc.setQueryData([...classifierMetricsKey("product", "acme-panel")], []);
    qc.setQueryData([...ME_KEY], admin);
    render(() => (
      <QueryClientProvider client={qc}>
        <Products />
      </QueryClientProvider>
    ));
    fireEvent.click(await screen.findByText("Acme Panel"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // Read mode: the icon key text is the override, not the type's monitor.
    expect(within(blade).getByText("tv")).toBeInTheDocument();
    expect(within(blade).queryByText("monitor")).not.toBeInTheDocument();
  });
});

// The catalog addresses rows by the name (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Products addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    const cell = screen.getByText("Acme Panel").closest("td")!;
    expect(within(cell).getByText("acme-panel")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "acme-panel" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Acme Panel")).toBeInTheDocument();
    expect(screen.queryByText("Crestron TSW-1070")).toBeNull();
  });
});

// The reference cells join on what the wire carries: vendor/driver cells show
// the name, never the uuid.
describe("Products reference honesty (#470)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders the vendor and driver handles in the catalog cells, never uuids", async () => {
    mount();
    expect(await screen.findByText("Crestron TSW-1070")).toBeInTheDocument();
    const row = screen.getByText("Crestron TSW-1070").closest("tr")!;
    expect(within(row).getByText("crestron")).toBeInTheDocument();
    expect(within(row).getByText("crestron-ct")).toBeInTheDocument();
    // No uuid leaks into the row.
    expect(within(row).queryByText(/00000000-0000-4000/)).toBeNull();
  });
});

// The catalog wears the shared identity cell (components/IdentityCell): one
// "Name" header whose cell carries the label over the handle, so a product reads
// the same here as it does on every other list. The separate "Name"
// column went with it: the cell already renders both.
describe("Products identity column", () => {
  it("carries both identities under one Name header", async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    const heads = screen.getAllByRole("columnheader").map((h) => h.textContent);
    expect(heads).toContain("Name");
    expect(heads.filter((h) => h === "Name")).toHaveLength(1);
    // Every other column survives, in order.
    expect(heads).toEqual(["Name", "Vendor", "Driver", "Kind", "Origin", ""]);

    const cell = screen.getByText("Acme Panel").closest("td")!;
    expect(within(cell).getByText("acme-panel")).toBeInTheDocument();
  });
});

// The create form leads with the display name and derives the handle from it, so
// an operator types "Acme Panel Pro" and never has to invent `acme-panel-pro` or
// think about the character class the API enforces. This proves the page is
// WIRED to the primitive; lib/entities.test.ts proves the suppression rule
// itself, which cannot be witnessed here (once a test types into an input its
// value property no longer tracks the signal).
describe("Products create identity", () => {
  const fields = async () => {
    mount();
    expect(await screen.findByText("Acme Panel")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new product/i }));
    const display = screen.getByPlaceholderText("Crestron TSW-1070") as HTMLInputElement;
    const key = screen.getByPlaceholderText("crestron-tsw-1070") as HTMLInputElement;
    return { display, key };
  };

  it("derives the handle as the display name is typed", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Acme Panel Pro" } });
    await waitFor(() => expect(key.value).toBe("acme-panel-pro"));
  });

  it("stops advertising the handle as derived once it is edited by hand", async () => {
    const { display, key } = await fields();
    fireEvent.input(display, { target: { value: "Acme Panel Pro" } });
    await waitFor(() => expect(key.value).toBe("acme-panel-pro"));

    fireEvent.input(key, { target: { value: "acme-panel-2" } });
    fireEvent.input(display, { target: { value: "Acme Panel Pro Mk2" } });

    await waitFor(() => expect(display.value).toBe("Acme Panel Pro Mk2"));
    expect(screen.getByText(/Globally unique address/)).toBeTruthy();
    expect(screen.queryByText(/Derived from the display name/)).toBeNull();
  });
});
