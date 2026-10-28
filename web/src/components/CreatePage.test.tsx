import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CreatePage from "./CreatePage";
import { TEMPLATES_KEY, type Template } from "../lib/templates";
import { ME_KEY, type Me } from "../lib/auth";
import { LOCATIONS_KEY, type Location } from "../lib/locations";
import { LOCATION_TYPES_KEY } from "../lib/location_types";
import { SYSTEMS_KEY } from "../lib/systems";
import { SYSTEM_TYPES_KEY } from "../lib/system_types";
import { COMPONENTS_KEY } from "../lib/components";
import { NODES_KEY } from "../lib/nodes";
import { STANDARDS_KEY, type Standard } from "../lib/standards";
import { PRODUCTS_KEY } from "../lib/products";
import { DRIVERS_KEY } from "../lib/drivers";
import { SECRETS_KEY } from "../lib/secrets";
import { TAGS_KEY } from "../lib/tags";
import { uuidFor } from "../lib/testids";

// The fleet create page's two ways in (#1329): from scratch (the default, the
// kind's own form) or from a template of the kind, inside the page rather than a
// trip to the Templates catalog. The template face lists only the kind's
// templates (a place template for a location), shows what the chosen one
// creates, asks only what it needs, posts the kind's :createFromTemplate, and
// lands on the created row's id with the notice. ?under= is the default place.

const catalog: Template[] = [
  { kind: "standard_template", name: "mr55", label: "Meeting Room 55", standard: { roles: [{ name: "main-display", label: "Main display", quorum: 1 }] } },
  { kind: "system_template", name: "mr55-system", label: "Meeting Room 55, staffed", system: { standard: "mr55", roles: { "main-display": { component: "boreal-edge-55" } } } },
  { kind: "place_template", name: "meeting-floor", label: "A floor of meeting rooms", place: { type: "floor", label: "Meeting floor" } },
  { kind: "component_template", name: "boreal-edge-55", label: "Boreal Edge 55 display", component: { product: "boreal-edge-55", inputs: { port: "161" } } },
];

const hq: Location = { id: uuidFor("l-hq"), name: "hq", label: "HQ", location_type: "campus", path: "hq" };
const west: Location = { id: uuidFor("l-west"), name: "west", label: "West", location_type: "building", parent_id: hq.id, path: "hq.west" };
const mr55: Standard = { id: uuidFor("std-mr55"), name: "mr55", label: "Meeting Room 55", official: false };

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
// The seeded deploy role over viewer's read floor: no standard:create, no endpoint:create.
const deploy: Me = {
  principal: { id: "u-deploy", kind: "human" }, human: { username: "deploy" }, grants: [],
  permissions: ["*:read", ...["location", "system", "component"].flatMap((r) => ["create", "update", "rename", "move"].map((a) => `${r}:${a}`))],
};

let landed = "";
function Landing() {
  const loc = useLocation();
  landed = `${loc.pathname}${loc.search}`;
  return <div data-testid="landed" />;
}

const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });

// Records every write; any read the caches do not answer gets an empty body.
function api(answers: Record<string, unknown>) {
  const posts: { url: string; body: unknown }[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.method === "POST") {
      const body = JSON.parse((await req.clone().text()) || "null");
      posts.push({ url: req.url, body });
      for (const [suffix, answer] of Object.entries(answers)) if (req.url.endsWith(suffix)) return json(answer, 201);
      return json({});
    }
    return json({});
  });
  return posts;
}

function mount(path: string, me: Me = admin, standards: Standard[] = []) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TEMPLATES_KEY], catalog);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...LOCATIONS_KEY], [hq, west]);
  qc.setQueryData([...LOCATION_TYPES_KEY], []);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...SYSTEM_TYPES_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...NODES_KEY], [{ name: "lab-node", enrolled: true, tags: {} }]);
  qc.setQueryData([...STANDARDS_KEY], standards);
  qc.setQueryData([...PRODUCTS_KEY], []);
  qc.setQueryData([...DRIVERS_KEY], []);
  qc.setQueryData([...SECRETS_KEY], []);
  qc.setQueryData([...TAGS_KEY], []);
  window.history.pushState({}, "", path);
  landed = "";
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/systems/create" component={() => <CreatePage kind="system" />} />
        <Route path="/locations/create" component={() => <CreatePage kind="location" />} />
        <Route path="/components/create" component={() => <CreatePage kind="component" />} />
        <Route path="/systems/:id" component={Landing} />
        <Route path="/locations/:id" component={Landing} />
        <Route path="/components/:id" component={Landing} />
      </Router>
    </QueryClientProvider>
  ));
}

const fromTemplate = async () => {
  fireEvent.click(await screen.findByRole("button", { name: "From a template" }));
  return (await screen.findByLabelText("Template")) as HTMLSelectElement;
};
const offered = (picker: HTMLSelectElement) => Array.from(picker.options).map((o) => o.value).filter(Boolean);

describe("CreatePage's From a template", () => {
  afterEach(() => vi.restoreAllMocks());

  it("starts From scratch, the kind's own form", async () => {
    api({});
    mount("/systems/create");
    expect(await screen.findByRole("button", { name: "From scratch" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByTestId("entity-create-form")).toBeInTheDocument();
    expect(screen.queryByTestId("template-create")).toBeNull();
  });

  it("creates a system from a template at the carried place and lands on its id", async () => {
    const posts = api({ "/systems:createFromTemplate": { template: "mr55-system", system: "meeting-1", system_id: uuidFor("sys-1"), standard: "mr55", standards_created: [], components: [], to_attach: [] } });
    mount(`/systems/create?under=${west.id}`, admin, [mr55]);
    const picker = await fromTemplate();
    expect(offered(picker)).toEqual(["mr55-system"]);
    expect(screen.queryByTestId("entity-create-form")).toBeNull();
    fireEvent.change(picker, { target: { value: "mr55-system" } });
    const form = await screen.findByTestId("start-form-system");
    expect(within(form).getByLabelText("Place")).toHaveValue("hq.west");
    fireEvent.click(screen.getByRole("button", { name: "Start system" }));
    await waitFor(() => expect(landed).toBe(`/systems/${uuidFor("sys-1")}?from=mr55-system`));
    // The scratch form's name preview may have posted before the switch; the one write is the create.
    expect(posts.filter((p) => p.url.includes("FromTemplate"))).toEqual([{ url: expect.stringMatching(/\/systems:createFromTemplate$/), body: { template: "mr55-system", location: "hq.west" } }]);
  });

  it("creates places from a place template under the carried parent", async () => {
    const posts = api({ "/locations:createFromTemplate": { template: "meeting-floor", places: [{ location: "level-9", location_id: uuidFor("l-9"), type: "floor" }], systems: [], standards_created: [], to_attach: [] } });
    mount(`/locations/create?under=${west.id}`);
    const picker = await fromTemplate();
    expect(offered(picker)).toEqual(["meeting-floor"]);
    fireEvent.change(picker, { target: { value: "meeting-floor" } });
    const form = await screen.findByTestId("start-form-place");
    fireEvent.input(within(form).getByLabelText("Name"), { target: { value: "level-9" } });
    fireEvent.click(screen.getByRole("button", { name: "Start places" }));
    await waitFor(() => expect(landed).toBe(`/locations/${uuidFor("l-9")}?from=meeting-floor`));
    expect(posts.find((p) => p.url.includes("FromTemplate"))).toEqual({ url: expect.stringMatching(/\/locations:createFromTemplate$/), body: { template: "meeting-floor", parent: "hq.west", name: "level-9" } });
  });

  it("creates a component from a template with its address", async () => {
    const posts = api({ "/components:createFromTemplate": { template: "boreal-edge-55", component: "display-1", component_id: uuidFor("c-1"), product: "boreal-edge-55", attached: true } });
    mount(`/components/create?under=${hq.id}`);
    const picker = await fromTemplate();
    expect(offered(picker)).toEqual(["boreal-edge-55"]);
    fireEvent.change(picker, { target: { value: "boreal-edge-55" } });
    const form = await screen.findByTestId("start-form-component");
    fireEvent.change(within(form).getByLabelText("Node"), { target: { value: "lab-node" } });
    fireEvent.input(within(form).getByLabelText("Address"), { target: { value: "10.0.0.20" } });
    fireEvent.click(screen.getByRole("button", { name: "Start component" }));
    await waitFor(() => expect(landed).toBe(`/components/${uuidFor("c-1")}?from=boreal-edge-55`));
    expect(posts.find((p) => p.url.includes("FromTemplate"))?.body).toEqual({ template: "boreal-edge-55", location: "hq", node: "lab-node", inputs: { host: "10.0.0.20" } });
  });

  it("refuses submit while the caller lacks what the start writes, naming it", async () => {
    api({});
    // mr55 is not held: the start would create it from its template, which deploy may not.
    mount("/systems/create", deploy);
    fireEvent.change(await fromTemplate(), { target: { value: "mr55-system" } });
    expect(await screen.findByTestId("start-missing")).toHaveTextContent("standard:create");
    expect(screen.getByRole("button", { name: "Start system" })).toBeDisabled();
  });

  it("hides the template choice from a caller who may not read templates", async () => {
    api({});
    mount("/systems/create", { ...admin, permissions: ["system:read", "system:create", "location:read"] });
    await screen.findByTestId("entity-create-form");
    expect(screen.queryByRole("button", { name: "From a template" })).toBeNull();
  });
});
