import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Variables from "./Variables";
import { VARIABLES_KEY, type Variable } from "../lib/variables";
import { LOCATIONS_KEY } from "../lib/locations";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";

// A variable at the `platform` tier is install-wide, so the server gates the write
// on `platform:<action>` on top of `variable:<action>`. The console must gate the
// same way: an estate writer (every variable action, at the all scope) holds full
// estate reach and no install-wide authority, so it must not be offered the
// Platform scope on the create form nor Edit / Delete on a tier row, and it should
// read which capability it is missing rather than earn a 403. Data is seeded into
// the query cache so no server is needed.
const seed: Variable[] = [
  { id: "v-tier", name: "poll_interval", value_type: "int", owner_kind: "platform", value: 60 },
  { id: "v-below", name: "room_poll", value_type: "int", owner_kind: "location", owner_name: "room", value: 30 },
];

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const estateWriter: Me = { principal: { id: "u-est", kind: "human" }, human: { username: "sam" }, permissions: ["variable:*"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = owner) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...VARIABLES_KEY], seed);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Variables />
    </QueryClientProvider>
  ));
}

async function openBlade(name: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByText(name));
  return waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
}

const scopeOptions = () =>
  Array.from((screen.getByLabelText("Scope") as HTMLSelectElement).options).map((o) => o.value);

describe("Variables page platform-tier authority", () => {
  afterEach(() => vi.restoreAllMocks());

  it("offers the Platform scope to a principal that holds the install-wide permission", async () => {
    mount(owner);
    expect(await screen.findByText("poll_interval")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new variable/i }));
    expect(scopeOptions()).toContain("platform");
    expect((screen.getByLabelText("Scope") as HTMLSelectElement).value).toBe("platform");
  });

  it("withholds the Platform scope from an estate writer and names the missing capability", async () => {
    mount(estateWriter);
    expect(await screen.findByText("poll_interval")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new variable/i }));
    // The control the server would refuse is not offered, and the default falls to
    // the most specific tier the caller may actually write at.
    expect(scopeOptions()).not.toContain("platform");
    expect((screen.getByLabelText("Scope") as HTMLSelectElement).value).not.toBe("platform");
    expect(screen.getByText(/platform:create/)).toBeInTheDocument();
  });

  it("hides Edit and Delete on a platform-tier row from an estate writer and says why", async () => {
    mount(estateWriter);
    expect(await screen.findByText("poll_interval")).toBeInTheDocument();
    const blade = await openBlade("poll_interval");
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).getByText(/platform:update/)).toBeInTheDocument();
    expect(within(blade).getByText(/platform:delete/)).toBeInTheDocument();
  });

  it("keeps Edit and Delete on a row below the tier for the same estate writer", async () => {
    mount(estateWriter);
    expect(await screen.findByText("room_poll")).toBeInTheDocument();
    const blade = await openBlade("room_poll");
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    expect(within(blade).queryByText(/platform:update/)).not.toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("keeps Edit and Delete on a platform-tier row for an owner", async () => {
    mount(owner);
    expect(await screen.findByText("poll_interval")).toBeInTheDocument();
    const blade = await openBlade("poll_interval");
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    expect(within(blade).queryByText(/platform:update/)).not.toBeInTheDocument();
  });
});
