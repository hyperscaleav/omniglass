import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Vendors from "./Vendors";
import { VENDORS_KEY, type Vendor } from "../lib/vendors";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Vendors is the manufacturer catalog on the flat FlatList surface (the
// vendor picker on the component_model form). An official (seed-owned) row is
// read-only, same invariant as the Types catalog's official rows: no edit
// pencil, no Delete. Data is seeded into the query cache so no server is needed.
const seed: Vendor[] = [
  { id: uuidFor("ven-crestron"), name: "crestron", display_name: "Crestron", kind: "manufacturer", official: true, icon: "crestron-logo" },
  { id: uuidFor("ven-acme-av"), name: "acme-av", display_name: "Acme AV", kind: "integrator", official: false, website: "https://acme.example" },
  { id: uuidFor("ven-evil-corp"), name: "evil-corp", display_name: "Evil Corp", kind: "manufacturer", official: false, website: "javascript:alert(document.cookie)" },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...VENDORS_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Vendors />
    </QueryClientProvider>
  ));
}

describe("Vendors page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists a seeded vendor, an official row has no edit/delete, and create opens for an admin", async () => {
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
    fireEvent.click(screen.getByRole("button", { name: /new vendor/i }));
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

  it("hides New vendor for a caller without vendor:create", () => {
    mount(viewer);
    expect(screen.queryByText(/New vendor/i)).toBeNull();
  });

  it("renders a normal https website as a live link", async () => {
    mount();
    fireEvent.click(screen.getByText("Acme AV"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const link = await within(blade).findByText("https://acme.example");
    expect(link.tagName).toBe("A");
    expect(link).toHaveAttribute("href", "https://acme.example/");
  });

  it("does not render a javascript: website as a live link (stored XSS guard)", async () => {
    mount();
    fireEvent.click(screen.getByText("Evil Corp"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    const value = await within(blade).findByText("javascript:alert(document.cookie)");
    expect(value.tagName).not.toBe("A");
    expect(blade.querySelector('a[href^="javascript:"]')).not.toBeInTheDocument();
  });
});

// The catalog addresses rows by the kebab handle (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Vendors addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Acme AV")).toBeInTheDocument();
    const row = screen.getByText("Acme AV").closest("tr")!;
    expect(within(row).getByText("acme-av")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "acme-av" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Acme AV")).toBeInTheDocument();
    expect(screen.queryByText("Crestron")).toBeNull();
  });
});
