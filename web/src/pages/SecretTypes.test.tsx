import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SecretTypes from "./SecretTypes";
import { SECRET_TYPES_KEY, type SecretType } from "../lib/secret_types";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Secret Types page is the release-owned registry of secret shapes, read-only
// by DESIGN, not by permission: the seed upserts it authoritatively (ON
// CONFLICT DO UPDATE) so a release can correct it, and no write route exists.
// The read-only-ness must therefore hold for an owner exactly as for anyone
// else: no create affordance, no edit pencil, no destructive action. The page
// is reached only by holders of secret:read (nav + route guard gate on the
// secret resource); a viewer-floor principal never mounts it, which the nav
// tests in lib/nav.test.ts prove from the other side.
const seed: SecretType[] = [
  {
    id: uuidFor("st-snmp-community"),
    name: "snmp-community",
    label: "SNMP Community",
    official: true,
    fields: [
      { name: "community", type: "string", secret: true, origin: "operator" },
    ],
  },
  { id: uuidFor("st-basic-auth"), name: "basic-auth", label: "Basic Auth", official: true, fields: [] },
];

const asides = () => document.querySelectorAll("aside[data-blade]");

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const operator: Me = { principal: { id: "u-op", kind: "human" }, human: { username: "op" }, permissions: ["*:read", "secret:read,reveal,create,update"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...SECRET_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <SecretTypes />
    </QueryClientProvider>
  ));
}

describe("SecretTypes page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the registry rows with name and label in one column", () => {
    mount();
    expect(screen.getByText("snmp-community")).toBeTruthy();
    expect(screen.getByText("basic-auth")).toBeTruthy();
    const cell = screen.getByText("snmp-community").closest("td");
    expect(cell?.textContent).toContain("SNMP Community");
  });

  // Read-only by design: even the owner (`>`), who holds every permission that
  // exists, gets no create affordance. Nothing on this page may mint a row.
  it("offers no create affordance, even to the owner", () => {
    mount(admin);
    expect(screen.queryByText(/New secret type/)).toBeNull();
    expect(screen.queryByText(/New type/)).toBeNull();
  });

  it("offers no create affordance to a secret-writing operator either", () => {
    mount(operator);
    expect(screen.queryByText(/New secret type/)).toBeNull();
  });

  it("blade shows the declared fields and a greyed Edit / Delete pair with the official reason, even for the owner", async () => {
    mount(admin);
    fireEvent.click(screen.getByText("snmp-community"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    // The fields list: name, scalar type, the secret marker, the origin.
    expect(within(blade).getByText("community")).toBeTruthy();
    expect(within(blade).getByText("string")).toBeTruthy();
    expect(within(blade).getByText("secret")).toBeTruthy();
    expect(within(blade).getByText("operator")).toBeTruthy();
    // The read-only note moved off the body into the locked pair's tooltip:
    // both buttons sit in their usual footer spots, greyed, and the tooltip
    // wrapper carries the shared official-row sentence (never seed language,
    // which is not operator vocabulary).
    expect(within(blade).queryByText(/seeded with the release and read-only/)).toBeNull();
    const editBtn = within(blade).getByLabelText("Edit") as HTMLButtonElement;
    const deleteBtn = within(blade).getByText("Delete").closest("button") as HTMLButtonElement;
    expect(editBtn.disabled).toBe(true);
    expect(deleteBtn.disabled).toBe(true);
    expect(editBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
    expect(deleteBtn.closest(".tooltip")?.getAttribute("data-tip")).toBe("Official: ships with Omniglass and updates with it.");
    // Greyed is not live: clicking enters no edit mode.
    fireEvent.click(editBtn);
    expect(within(blade).queryByText("Save")).toBeNull();
  });

  it("blade renders an empty fields list with its placeholder", async () => {
    mount();
    fireEvent.click(screen.getByText("basic-auth"));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("No fields declared.")).toBeTruthy();
  });
});
