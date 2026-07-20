import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Vendors from "./Vendors";
import { VENDORS_KEY, type Vendor } from "../lib/vendors";
import { ME_KEY, type Me } from "../lib/auth";

// Vendors is the manufacturer catalog on the flat FlatList surface (the
// vendor picker on the component_model form). An official (seed-owned) row is
// read-only, same invariant as the Types catalog's official rows: no edit
// pencil, no Delete. Data is seeded into the query cache so no server is needed.
const seed: Vendor[] = [
  { id: "crestron", display_name: "Crestron", kind: "manufacturer", official: true, icon: "crestron-logo" },
  { id: "acme-av", display_name: "Acme AV", kind: "integrator", official: false, website: "https://acme.example" },
  { id: "evil-corp", display_name: "Evil Corp", kind: "manufacturer", official: false, website: "javascript:alert(document.cookie)" },
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
