import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Templates from "./Templates";
import { TEMPLATES_KEY, type Template } from "../lib/templates";
import { ME_KEY, type Me } from "../lib/auth";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { NODES_KEY } from "../lib/nodes";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { DRIVERS_KEY } from "../lib/drivers";
import { SECRETS_KEY } from "../lib/secrets";
import { uuidFor } from "../lib/testids";
import { standardRolesKey } from "../lib/system_roles";

// The Templates page (#1329): the catalog across four kinds, filtered by the
// URL's ?kind=, each row a blade that renders what the template creates from
// its own body, and "Start from this template" (gated by what it creates)
// opening the kind's form, whose submit posts the kind's route and lands on the
// created row with ?from=. Data is seeded into the query cache.

const catalog: Template[] = [
  {
    kind: "standard_template", name: "mr55", label: "Meeting Room 55", description: "A 55-inch meeting room.",
    standard: {
      parent: "meeting-room",
      roles: [
        { name: "video-bar", label: "Video bar", quorum: 1, capacity: 1, accepted_types: ["video-bar"] },
        { name: "main-display", label: "Main display", quorum: 1, capacity: 1, pinned_products: ["boreal-edge-55"] },
      ],
      choices: [{ name: "audio", label: "Audio", alternates: [{ name: "bar", label: "From the bar" }, { name: "dsp", label: "A DSP" }] }],
    },
  },
  {
    kind: "system_template", name: "mr55-system", label: "Meeting Room 55, staffed",
    system: { standard: "mr55", system_type: "meeting", roles: { "video-bar": { component: "kestrel-vroom" }, "main-display": { component: "boreal-edge-55" } } },
  },
  {
    kind: "place_template", name: "meeting-floor", label: "A floor of meeting rooms",
    place: { type: "floor", label: "Meeting floor", places: [{ type: "room", name: "mr-1", label: "Meeting room 1", systems: [{ template: "mr55-system" }] }] },
  },
  { kind: "component_template", name: "boreal-edge-55", label: "Boreal Edge 55 display", component: { product: "boreal-edge-55", inputs: { port: "161" } } },
  { kind: "component_template", name: "kestrel-vroom", label: "Kestrel VRoom video bar", component: { product: "kestrel-vroom", inputs: { port: "443", "tls-mode": "pin" } } },
];

// Places are offered and posted by dotted path; the create flows carry a uuid.
const hq: Location = { id: uuidFor("l-hq"), name: "hq", label: "HQ", location_type: "campus", path: "hq" };
const west: Location = { id: uuidFor("l-west"), name: "west", label: "West", location_type: "building", parent_id: hq.id, path: "hq.west" };
const standards: Standard[] = [{ id: uuidFor("std-mr"), name: "meeting-room", label: "Meeting room", official: false }];

const SYS_ID = uuidFor("sys-meeting-1");
const LVL_ID = uuidFor("l-level-9");
const COMP_ID = uuidFor("c-display-1");

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

let landed = "";
function Landing() {
  const loc = useLocation();
  landed = `${loc.pathname}${loc.search}`;
  return <div data-testid="landed" />;
}

// A deploy-shaped caller (the seeded deploy role over viewer's read floor):
// location, system and component create/update/rename/move, no standard:create,
// no endpoint:create.
const deploy: Me = {
  principal: { id: "u-deploy", kind: "human" }, human: { username: "deploy" }, grants: [],
  permissions: ["*:read", ...["location", "system", "component"].flatMap((r) => ["create", "update", "rename", "move"].map((a) => `${r}:${a}`))],
};

// The fleet's own mr55, whose roles differ from the template's copy.
const mr55: Standard = { id: uuidFor("std-mr55"), name: "mr55", label: "Meeting Room 55", official: false };

function mount(path: string, me: Me = admin, held: Standard[] = standards, roles: Record<string, unknown[]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TEMPLATES_KEY], catalog);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATIONS_KEY], [hq, west]);
  qc.setQueryData([...NODES_KEY], [{ name: "lab-node", enrolled: true, tags: {} }]);
  qc.setQueryData([...STANDARDS_KEY], held);
  for (const [id, r] of Object.entries(roles)) qc.setQueryData([...standardRolesKey(id)], r);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...DRIVERS_KEY], []);
  qc.setQueryData([...SECRETS_KEY], []);
  window.history.pushState({}, "", path);
  landed = "";
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/templates" component={Templates} />
        <Route path="/systems/:id" component={Landing} />
        <Route path="/standards" component={Landing} />
        <Route path="/locations/:id" component={Landing} />
        <Route path="/components/:id" component={Landing} />
      </Router>
    </QueryClientProvider>
  ));
}

const blade = async (rowText: string): Promise<HTMLElement> => {
  fireEvent.click(await screen.findByText(rowText));
  return await waitFor(() => {
    const el = document.querySelector("aside[data-blade]");
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
};

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

describe("Templates page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists every kind, and teaches what a template is on the rail", async () => {
    mount("/templates");
    expect(await screen.findByText("Meeting Room 55")).toBeInTheDocument();
    expect(screen.getByText("A floor of meeting rooms")).toBeInTheDocument();
    expect(screen.getByText("Boreal Edge 55 display")).toBeInTheDocument();
    expect(screen.getByTestId("template-teach")).toHaveTextContent("An example you clone, then own");
    expect(screen.getByText("2 roles, a variant of meeting-room")).toBeInTheDocument();
  });

  it("filters to the kinds the URL asks for, the chip reading the kind's word", async () => {
    mount("/templates?kind=system_template");
    expect(await screen.findByText("Meeting Room 55, staffed")).toBeInTheDocument();
    expect(screen.queryByText("Meeting Room 55")).toBeNull();
    expect(screen.queryByText("Boreal Edge 55 display")).toBeNull();
    expect(screen.getAllByText("system").length).toBeGreaterThan(0);
  });

  it("renders a standard template's roles and choices from its body", async () => {
    mount("/templates?kind=standard_template");
    const b = await blade("Meeting Room 55");
    const preview = within(b).getByTestId("preview-standard");
    expect(within(preview).getByText("Video bar")).toBeInTheDocument();
    expect(within(preview).getByText("boreal-edge-55")).toBeInTheDocument();
    expect(within(preview).getByText("From the bar (default)")).toBeInTheDocument();
    expect(within(b).getByText("A 55-inch meeting room.")).toBeInTheDocument();
  });

  it("renders a system's role fills by their component templates' labels", async () => {
    mount("/templates?kind=system_template");
    const b = await blade("Meeting Room 55, staffed");
    const preview = within(b).getByTestId("preview-system");
    expect(within(preview).getByText("Kestrel VRoom video bar")).toBeInTheDocument();
    expect(within(preview).getByText("Main display")).toBeInTheDocument();
  });

  it("renders a place template as an indented tree with the systems that land in it", async () => {
    mount("/templates?kind=place_template");
    const b = await blade("A floor of meeting rooms");
    const items = within(b).getAllByRole("treeitem");
    expect(items.map((i) => i.getAttribute("data-depth"))).toEqual(["0", "1", "2"]);
    expect(items[2]).toHaveTextContent("Meeting Room 55, staffed");
  });

  it("renders a component's product and preset inputs", async () => {
    mount("/templates?kind=component_template");
    const b = await blade("Kestrel VRoom video bar");
    expect(within(b).getByText("tls-mode = pin")).toBeInTheDocument();
  });

  it("offers Start only to a caller who may create what it makes", async () => {
    mount("/templates?kind=system_template", viewer);
    const b = await blade("Meeting Room 55, staffed");
    expect(within(b).queryByRole("button", { name: /start from this template/i })).toBeNull();
    // Every write the start makes, not only the route's own gate.
    expect(within(b).getByTestId("start-needs")).toHaveTextContent("needs system:create, system:update, component:create, component:update, standard:create");
  });

  it("starts a system at the carried place and lands on it with ?from=", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.endsWith("/systems:createFromTemplate")) {
        sent = JSON.parse(await req.clone().text());
        return json({ template: "mr55-system", system: "meeting-1", system_id: SYS_ID, standard: "mr55", standards_created: ["meeting-room", "mr55"], components: [], to_attach: ["display-1"] }, 201);
      }
      return json({});
    });
    mount(`/templates?kind=system_template&place=${west.id}`);
    const b = await blade("Meeting Room 55, staffed");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-system");
    // An address with no node is refused before submit.
    fireEvent.input(within(form).getAllByPlaceholderText("unattached")[0], { target: { value: "10.0.0.5" } });
    expect(await within(form).findByText("An address needs a node to place its endpoint on.")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /start system/i })).toBeDisabled();
    fireEvent.change(within(form).getByLabelText("Node"), { target: { value: "lab-node" } });
    fireEvent.click(screen.getByRole("button", { name: /start system/i }));
    // Lands on the id the answer carries, with the standards it created and what it left to attach.
    await waitFor(() => expect(landed).toBe(`/systems/${SYS_ID}?created_standards=meeting-room%2Cmr55&unattached=1&from=mr55-system`));
    expect(sent).toEqual({ template: "mr55-system", location: "hq.west", node: "lab-node", attach: { "video-bar": [{ host: "10.0.0.5" }] } });
  });

  it("shows the API's refusal and lands nowhere", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(async () =>
      json({ title: "Unprocessable Entity", status: 422, detail: "mr55 is a variant of meeting-room: create a standard from meeting-room first, or name the standard it hangs off as parent" }, 422),
    );
    mount("/templates?kind=standard_template");
    const b = await blade("Meeting Room 55");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-standard");
    expect(within(form).getByLabelText("Variant of")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /create standard/i }));
    expect(await within(form).findByRole("alert")).toHaveTextContent("create a standard from meeting-room first");
    expect(landed).toBe("");
  });

  it("creates a standard from its template and lands on its blade with ?from=", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      sent = JSON.parse(await req.clone().text());
      return json({ id: uuidFor("std-east"), name: "mr55-east", label: "East", official: false }, 201);
    });
    mount("/templates?kind=standard_template");
    const b = await blade("Meeting Room 55");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-standard");
    fireEvent.input(within(form).getByPlaceholderText("mr55"), { target: { value: "mr55-east" } });
    fireEvent.click(screen.getByRole("button", { name: /create standard/i }));
    await waitFor(() => expect(landed).toBe("/standards?open=mr55-east&from=mr55"));
    expect(sent).toEqual({ template: "mr55", name: "mr55-east" });
  });

  it("starts places under a parent and lands on the subtree's root", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        sent = JSON.parse(await req.clone().text());
        return json({ template: "meeting-floor", places: [{ location: "level-9", location_id: LVL_ID, type: "floor", parent: "west" }], systems: [], standards_created: [], to_attach: [] }, 201);
      }
      return json({});
    });
    mount("/templates?kind=place_template");
    const b = await blade("A floor of meeting rooms");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-place");
    fireEvent.change(within(form).getByLabelText("Parent place"), { target: { value: "hq.west" } });
    fireEvent.input(within(form).getByLabelText("Name"), { target: { value: "level-9" } });
    fireEvent.click(screen.getByRole("button", { name: /start places/i }));
    await waitFor(() => expect(landed).toBe(`/locations/${LVL_ID}?from=meeting-floor`));
    expect(sent).toEqual({ template: "meeting-floor", parent: "hq.west", name: "level-9" });
  });

  it("starts a component with its address on the host input", async () => {
    let sent: unknown;
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const req = input as Request;
      if (req.method === "POST") {
        sent = JSON.parse(await req.clone().text());
        return json({ template: "boreal-edge-55", component: "display-1", component_id: COMP_ID, product: "boreal-edge-55", attached: true }, 201);
      }
      return json({});
    });
    mount(`/templates?kind=component_template&place=${hq.id}`);
    const b = await blade("Boreal Edge 55 display");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-component");
    fireEvent.change(within(form).getByLabelText("Node"), { target: { value: "lab-node" } });
    fireEvent.input(within(form).getByLabelText("Address"), { target: { value: "10.0.0.20" } });
    fireEvent.click(screen.getByRole("button", { name: /start component/i }));
    await waitFor(() => expect(landed).toBe(`/components/${COMP_ID}?from=boreal-edge-55`));
    expect(sent).toEqual({ template: "boreal-edge-55", location: "hq", node: "lab-node", inputs: { host: "10.0.0.20" } });
  });

  it("names everything a deploy-shaped caller lacks, and offers no Start", async () => {
    // mr55 is not held, so starting would create it from its template: standard:create, which deploy lacks.
    mount("/templates?kind=system_template", deploy);
    const b = await blade("Meeting Room 55, staffed");
    expect(within(b).queryByRole("button", { name: /start from this template/i })).toBeNull();
    expect(within(b).getByTestId("start-needs")).toHaveTextContent("needs standard:create");
  });

  it("lets deploy start a system to a held standard, with no address fields to attach", async () => {
    mount("/templates?kind=system_template", deploy, [...standards, mr55], { [mr55.id]: [] });
    const b = await blade("Meeting Room 55, staffed");
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-system");
    expect(within(form).queryAllByPlaceholderText("unattached")).toHaveLength(0);
    expect(within(form).getByPlaceholderText(/attaching needs endpoint:create/)).toBeDisabled();
    expect(within(form).queryByTestId("start-missing")).toBeNull();
  });

  it("refuses a standard deploy cannot create, naming the permission", async () => {
    mount("/templates?kind=standard_template", deploy);
    const b = await blade("Meeting Room 55");
    expect(within(b).getByTestId("start-needs")).toHaveTextContent("needs standard:create");
  });

  it("staffs the address slots to the fleet's own standard when it holds one", async () => {
    const roles = [
      { name: "video-bar", label: "Video bar", quorum: 1, accepted_types: [], pinned_products: [], position_labels: [], impact: "degraded" },
      { name: "main-display", label: "Main display", quorum: 2, accepted_types: [], pinned_products: [], position_labels: [], impact: "degraded" },
    ];
    mount("/templates?kind=system_template", admin, [...standards, mr55], { [mr55.id]: roles });
    const b = await blade("Meeting Room 55, staffed");
    expect(within(b).getByTestId("preview-standard-source")).toHaveTextContent("(yours)");
    expect(within(b).getByText("x2")).toBeInTheDocument();
    fireEvent.click(within(b).getByRole("button", { name: /start from this template/i }));
    const form = await screen.findByTestId("start-form-system");
    expect(within(form).getByText("Main display 1")).toBeInTheDocument();
    expect(within(form).getByText("Main display 2")).toBeInTheDocument();
  });
});
