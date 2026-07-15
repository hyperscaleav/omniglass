import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import ComponentMakes from "./ComponentMakes";
import { COMPONENT_MAKES_KEY, type ComponentMake } from "../lib/component_makes";
import { ME_KEY, type Me } from "../lib/auth";

// ComponentMakes is the manufacturer catalog on the flat FlatList surface (the
// make picker on the component_model form). An official (seed-owned) row is
// read-only, same invariant as the Types catalog's official rows: no edit
// pencil, no Delete. Data is seeded into the query cache so no server is needed.
const seed: ComponentMake[] = [
  { id: "crestron", display_name: "Crestron", official: true, icon: "crestron-logo" },
  { id: "acme-av", display_name: "Acme AV", official: false, website: "https://acme.example" },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMPONENT_MAKES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <ComponentMakes />
    </QueryClientProvider>
  ));
}

describe("ComponentMakes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists a seeded make, an official row has no edit/delete, and create opens for an admin", async () => {
    mount();
    expect(await screen.findByText("Crestron")).toBeInTheDocument();

    // official row has no edit/delete
    fireEvent.click(screen.getByText("Crestron"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();

    // create is available to an admin
    fireEvent.click(screen.getByRole("button", { name: /new make/i }));
    expect(screen.getByLabelText(/display name/i)).toBeInTheDocument();
  });

  it("a custom (non-official) row carries edit and delete", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme AV"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("hides New make for a caller without make:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New make/i)).toBeNull();
  });
});
