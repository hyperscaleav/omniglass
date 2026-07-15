import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ComponentModels from "./ComponentModels";
import { COMPONENT_MODELS_KEY, type ComponentModel } from "../lib/component_models";
import { COMPONENT_MAKES_KEY, type ComponentMake } from "../lib/component_makes";
import { ME_KEY, type Me } from "../lib/auth";

// ComponentModels is the product catalog on the flat FlatList surface: one row
// per make + model, with lifecycle fields and front/back product photos. An
// official (seed-owned) row is read-only, same invariant as Makes and Types:
// no edit pencil, no Delete. Data is seeded into the query cache so no server
// is needed.
const makes: ComponentMake[] = [
  { id: "crestron", display_name: "Crestron", official: true },
  { id: "biamp", display_name: "Biamp", official: true },
];

const seed: ComponentModel[] = [
  { id: "acme-123a", display_name: "Acme 123A", make_id: "crestron", model_number: "123A", official: false },
  { id: "biamp-x", display_name: "Biamp X", make_id: "biamp", model_number: "X-1", official: true },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENT_MODELS_KEY], seed);
  qc.setQueryData([...COMPONENT_MAKES_KEY], makes);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ComponentModels />
    </QueryClientProvider>
  ));
}

describe("ComponentModels page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists models with the resolved make name and filters by make", async () => {
    mount();
    expect(await screen.findByText("Acme 123A")).toBeInTheDocument();
    expect(screen.getByText("Biamp X")).toBeInTheDocument();
    // Make column resolves the make_id to its display name, not the raw id.
    expect(screen.getByText("Crestron")).toBeInTheDocument();
    expect(screen.getByText("Biamp")).toBeInTheDocument();

    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "make:crestron" } });
    fireEvent.keyDown(input, { key: "Enter" });

    expect(screen.getByText("Acme 123A")).toBeInTheDocument();
    expect(screen.queryByText("Biamp X")).not.toBeInTheDocument();
  });

  it("an official row has no edit/delete", async () => {
    mount();
    fireEvent.click(screen.getByText("Biamp X"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();
  });

  it("a custom (non-official) row carries edit and delete", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme 123A"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("the create form opens for an admin, with a make picker and front/back image inputs", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new model/i }));
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/make/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/front image/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/back image/i)).toBeInTheDocument();
  });

  it("hides New model for a caller without model:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New model/i)).toBeNull();
  });
});
