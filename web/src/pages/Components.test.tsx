import { describe, it, expect, afterEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Components from "./Components";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { SYSTEMS_KEY } from "../lib/systems";
import { LOCATIONS_KEY } from "../lib/locations";
import { ME_KEY, type Me } from "../lib/auth";
import { TAGS_KEY, entityTagsKey } from "../lib/tags";

// The Components page on the shared TreeList in the create-as-route model: New routes
// to /components/create (a draft accordion), Save hands off to /components/<name> in
// edit; the detail is read-only in view (no in-body mutation control) and editable via
// the pencil. Data is seeded into the query cache so no server is needed; `>` grants
// every permission.
const me: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const comp: Component = { id: "c-1", name: "mic-2", display_name: "Ceiling Mic 2", component_type: "microphone", effective_tags: {} };

function mount(path: string) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENTS_KEY], [comp]);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...TAGS_KEY], []);
  qc.setQueryData([...entityTagsKey("component", "mic-2")], []);
  window.history.pushState({}, "", path);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/components" component={Components} />
        <Route path="/components/:name" component={Components} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("Components create-as-route", () => {
  afterEach(() => window.history.pushState({}, "", "/"));

  it("renders the draft-create accordion at /components/create", async () => {
    mount("/components/create");
    await waitFor(() => expect(screen.getByText("New component")).toBeTruthy());
    expect(screen.getByText("Draft")).toBeTruthy();
    expect(screen.getByText("Create component")).toBeTruthy();
    // Identity + Placement fields present; the binding sections are locked.
    expect(screen.getByText("Name")).toBeTruthy();
    expect(screen.getByText("Component type")).toBeTruthy();
    expect(screen.getByText("System")).toBeTruthy();
    expect(screen.getByText(/Available once the component is created/)).toBeTruthy();
  });

  it("shows an existing component read-only in view: no tag add control, an Edit affordance", async () => {
    mount("/components/mic-2");
    // The detail resolves and renders the read-only facts.
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    // No in-body mutation control in view: the TagAdder add row is absent.
    expect(screen.queryByPlaceholderText(/Add a tag/)).toBeNull();
    // The view footer offers Edit (which would flip the accordion to edit mode).
    expect(screen.getByText("Edit")).toBeTruthy();
  });

  it("edit mode exposes an editable technical name with a check button", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Edit")).toBeTruthy());
    fireEvent.click(screen.getByText("Edit"));
    // The technical name becomes an editable input seeded from the row.
    const nameInput = (await screen.findByDisplayValue("mic-2")) as HTMLInputElement;
    expect(nameInput.disabled).toBe(false);
    // An inline check button sits beside it.
    expect(screen.getByLabelText("Check name")).toBeTruthy();
  });

  it("a fresh detail view keeps the technical name read-only", async () => {
    mount("/components/mic-2");
    await waitFor(() => expect(screen.getByText("Technical name")).toBeTruthy());
    // No check button until edit begins: the name is a read-only fact.
    expect(screen.queryByLabelText("Check name")).toBeNull();
  });
});
