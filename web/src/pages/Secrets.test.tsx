import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Secrets from "./Secrets";
import { SECRETS_KEY, type Secret } from "../lib/secrets";
import { SECRET_TYPES_KEY, type SecretType } from "../lib/secret_types";
import { LOCATIONS_KEY } from "../lib/locations";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// A secret at the `platform` tier is install-wide, so the server gates the write on
// `platform:<action>` on top of `secret:<action>`. The console must gate the same
// way: a fleet writer (every secret action, at the all scope) holds full fleet
// reach and no install-wide authority, so it must not be offered the Platform scope
// on the create form nor Edit / Delete on a tier row, and it should read which
// capability it is missing rather than earn a 403. Same treatment as the Settings
// page, which meets the same paired gate.
const types: SecretType[] = [
  { id: uuidFor("snmp-community"), name: "snmp-community", label: "SNMP community", official: true, fields: [{ name: "community", type: "string", secret: true, origin: "operator" }] },
];

const seed: Secret[] = [
  { id: uuidFor("s-tier"), name: "poll_community", secret_type: "snmp-community", owner_kind: "platform", fields: [{ name: "community", value: "••••••", secret: true }] },
  { id: uuidFor("s-below"), name: "room_community", secret_type: "snmp-community", owner_kind: "location", owner_name: "room", fields: [{ name: "community", value: "••••••", secret: true }] },
];

const owner: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const fleetWriter: Me = { principal: { id: "u-est", kind: "human" }, human: { username: "sam" }, permissions: ["secret:>"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = owner) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...SECRETS_KEY], seed);
  qc.setQueryData([...SECRET_TYPES_KEY], types);
  qc.setQueryData([...LOCATIONS_KEY], []);
  qc.setQueryData([...SYSTEMS_KEY], []);
  qc.setQueryData([...COMPONENTS_KEY], []);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Secrets />
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

describe("Secrets page platform-tier authority", () => {
  afterEach(() => vi.restoreAllMocks());

  it("posts the type handle, never the uuid, when creating a secret (#467)", async () => {
    const calls: { url: string; body: unknown }[] = [];
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const req = input as Request;
      calls.push({ url: req.url, body: req.body ? await req.clone().json() : undefined });
      return new Response(JSON.stringify({}), { status: 201, headers: { "content-type": "application/json" } });
    }));
    mount(owner);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new secret/i }));
    const typeSelect = screen.getByLabelText("Type") as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: typeSelect.options[1]?.value ?? typeSelect.options[0].value } });
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "poll" } });
    fireEvent.input(screen.getByLabelText(/community/i), { target: { value: "s3cr3t" } });
    const submitBtn = screen.getByRole("button", { name: /create secret/i });
    expect(submitBtn).not.toBeDisabled();
    fireEvent.click(submitBtn);
    await waitFor(() => {
      const post = calls.find((c) => c.url.includes("/secrets"));
      if (!post) throw new Error("no create call yet");
      expect((post.body as { secret_type: string }).secret_type).toBe("snmp-community");
    });
  });

  it("offers the Platform scope to a principal that holds the install-wide permission", async () => {
    mount(owner);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new secret/i }));
    expect(scopeOptions()).toContain("platform");
    expect((screen.getByLabelText("Scope") as HTMLSelectElement).value).toBe("platform");
  });

  it("withholds the Platform scope from a fleet writer and names the missing capability", async () => {
    mount(fleetWriter);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new secret/i }));
    expect(scopeOptions()).not.toContain("platform");
    expect((screen.getByLabelText("Scope") as HTMLSelectElement).value).not.toBe("platform");
    expect(screen.getByText(/platform:create/)).toBeInTheDocument();
  });

  it("hides Edit and Delete on a platform-tier row from a fleet writer and says why", async () => {
    mount(fleetWriter);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    const blade = await openBlade("poll_community");
    expect(within(blade).queryByLabelText("Edit")).not.toBeInTheDocument();
    expect(within(blade).queryByRole("button", { name: /delete/i })).not.toBeInTheDocument();
    expect(within(blade).getByText(/platform:update/)).toBeInTheDocument();
    expect(within(blade).getByText(/platform:delete/)).toBeInTheDocument();
  });

  it("keeps Edit and Delete on a row below the tier for the same fleet writer", async () => {
    mount(fleetWriter);
    expect(await screen.findByText("room_community")).toBeInTheDocument();
    const blade = await openBlade("room_community");
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    expect(within(blade).queryByText(/platform:update/)).not.toBeInTheDocument();
    fireEvent.click(within(blade).getByLabelText("Edit"));
    expect(within(blade).getByRole("button", { name: /delete/i })).toBeInTheDocument();
  });

  it("keeps Edit and Delete on a platform-tier row for an owner", async () => {
    mount(owner);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    const blade = await openBlade("poll_community");
    expect(within(blade).getByLabelText("Edit")).toBeInTheDocument();
    expect(within(blade).queryByText(/platform:update/)).not.toBeInTheDocument();
  });
});

// A successful create lands the operator on the new row (#471). Secrets keys
// its rows by uuid, so the page must hand the API-returned row (not the typed
// name) to the blade opener.
describe("Secrets create lands on the new row (#471)", () => {
  it("opens the created secret's blade after a successful create", async () => {
    const created: Secret = { id: uuidFor("s-new"), name: "door_code", secret_type: "snmp-community", owner_kind: "platform", fields: [] };
    vi.stubGlobal("fetch", vi.fn(async (input: RequestInfo | URL) => {
      const req = input as Request;
      if (req.method === "POST" && req.url.includes("/secrets")) {
        return new Response(JSON.stringify(created), { status: 201, headers: { "content-type": "application/json" } });
      }
      // The post-create invalidation refetch sees the new row.
      return new Response(JSON.stringify({ secrets: [...seed, created] }), { status: 200, headers: { "content-type": "application/json" } });
    }));
    mount(owner);
    expect(await screen.findByText("poll_community")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: /new secret/i }));
    const typeSelect = screen.getByLabelText("Type") as HTMLSelectElement;
    fireEvent.change(typeSelect, { target: { value: "snmp-community" } });
    fireEvent.input(screen.getByLabelText("Name"), { target: { value: "door_code" } });
    fireEvent.input(screen.getByLabelText(/community/i), { target: { value: "s3cr3t" } });
    fireEvent.click(screen.getByRole("button", { name: /create secret/i }));
    const blade = await waitFor(() => {
      const el = asides()[0];
      if (!el) throw new Error("no blade yet");
      return el as HTMLElement;
    });
    expect(within(blade).getByText("door_code")).toBeInTheDocument();
  });
});

// #627 scopes name uniqueness to placement: two locations (or components) may
// legally share a name. The create form's owner picker used to key its
// TreeSelect items on the bare name, so two same-named candidates rendered as
// identical, value-indistinguishable options and the posted owner was an
// ambiguous bare name a scoped write would now 409 on.
describe("Secrets create owner picker survives duplicate names (#627)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("offers two same-named location owners as distinct, independently selectable options", async () => {
    const annexA = { id: uuidFor("loc-annex-a"), name: "annex", location_type: "campus" };
    const annexB = { id: uuidFor("loc-annex-b"), name: "annex", location_type: "campus" };
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...SECRETS_KEY], []);
    qc.setQueryData([...SECRET_TYPES_KEY], types);
    qc.setQueryData([...LOCATIONS_KEY], [annexA, annexB]);
    qc.setQueryData([...SYSTEMS_KEY], []);
    qc.setQueryData([...COMPONENTS_KEY], []);
    qc.setQueryData([...ME_KEY], owner);
    render(() => (
      <QueryClientProvider client={qc}>
        <Secrets />
      </QueryClientProvider>
    ));
    fireEvent.click(screen.getByRole("button", { name: /new secret/i }));
    fireEvent.change(screen.getByLabelText("Scope"), { target: { value: "location" } });
    const ownerSelect = (await screen.findByLabelText("Location")) as HTMLSelectElement;
    const values = Array.from(ownerSelect.options).map((o) => o.value);
    expect(values).toContain(annexA.id);
    expect(values).toContain(annexB.id);

    fireEvent.change(ownerSelect, { target: { value: annexA.id } });
    expect(ownerSelect.value).toBe(annexA.id);
    fireEvent.change(ownerSelect, { target: { value: annexB.id } });
    expect(ownerSelect.value).toBe(annexB.id);
  });
});
