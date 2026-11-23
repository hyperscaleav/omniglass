import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import MoveEntity from "./MoveEntity";
import { pickInPicker } from "./ui/Picker.testkit";
import type { Member } from "../lib/members";

// "Move to..." (#1804): the explicit relocation, its own act. Move calls the
// move route (or, for a component's system, the planned membership steps), a
// refusal reads in place, and nothing here is the "Also serves..." add.

const moveComponent = vi.fn();
const moveSystem = vi.fn();
const applySteps = vi.fn();
vi.mock("../lib/components", async (orig) => ({ ...(await orig<typeof import("../lib/components")>()), moveComponent: (...a: unknown[]) => moveComponent(...a) }));
vi.mock("../lib/systems", async (orig) => ({ ...(await orig<typeof import("../lib/systems")>()), moveSystem: (...a: unknown[]) => moveSystem(...a) }));
vi.mock("../lib/members", async (orig) => ({ ...(await orig<typeof import("../lib/members")>()), applyMembershipSteps: (...a: unknown[]) => applySteps(...a) }));
const systemSlots = vi.fn(async (_: string) => [] as unknown[]);
vi.mock("../lib/system_slots", async (orig) => ({ ...(await orig<typeof import("../lib/system_slots")>()), systemSlots: (s: string) => systemSlots(s) }));

const places = [
  { value: "room-a", label: "Boardroom A", detail: "HQ / West" },
  { value: "room-b", label: "Boardroom B", detail: "HQ / East" },
];
const member = (system_id: string, primary: boolean): Member => ({ component: "dsp-1", component_id: "c1", component_health: "healthy", primary, system: system_id, system_id, system_count: 1 });

function mount(extra: Partial<Parameters<typeof MoveEntity>[0]> = {}) {
  const onMoved = vi.fn();
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  render(() => (
    <QueryClientProvider client={qc}>
      <MoveEntity kind="component" id="c1" places={places} parents={[{ value: "p1", label: "Rack 1" }]} onMoved={onMoved} {...extra} />
    </QueryClientProvider>
  ));
  return { onMoved, qc };
}
const openPanel = () => fireEvent.click(screen.getByRole("button", { name: "Move to..." }));
const destination = () => screen.getByRole("combobox", { name: "Destination" });

afterEach(() => { cleanup(); vi.clearAllMocks(); });

describe("MoveEntity", () => {
  it("moves a component to another place through the move route, then refreshes", async () => {
    moveComponent.mockResolvedValue({});
    const { onMoved } = mount({ currentPlace: "room-a" });
    openPanel();
    await pickInPicker(destination(), "Boardroom B");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => expect(moveComponent).toHaveBeenCalledWith("c1", { location: "room-b" }));
    expect(applySteps).not.toHaveBeenCalled();
    await waitFor(() => expect(onMoved).toHaveBeenCalled());
    expect((await screen.findByRole("status")).textContent).toBe("Moved to Boardroom B.");
  });

  it("moves a system under another system with the parent field", async () => {
    moveSystem.mockResolvedValue({});
    mount({ kind: "system", id: "s1", parents: [{ value: "s2", label: "Hall", detail: "Hall place" }] });
    openPanel();
    fireEvent.click(screen.getByRole("radio", { name: "Under a system" }));
    await pickInPicker(destination(), "Hall");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => expect(moveSystem).toHaveBeenCalledWith("s1", { parent: "s2" }));
    expect(moveComponent).not.toHaveBeenCalled();
  });

  it("offers No parent only when it has one, and sends the clear as an empty string", async () => {
    moveSystem.mockResolvedValue({});
    mount({ kind: "system", id: "s1", currentParent: "s2", parents: [{ value: "s2", label: "Hall" }, { value: "s3", label: "Annex Hall" }] });
    openPanel();
    fireEvent.click(screen.getByRole("radio", { name: "Under a system" }));
    await pickInPicker(destination(), "No parent (top level)");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => expect(moveSystem).toHaveBeenCalledWith("s1", { parent: "" }));
  });

  it("moves a component to another system by leaving the old one, freeing the slot it held", async () => {
    applySteps.mockResolvedValue(undefined);
    systemSlots.mockImplementation(async (s: string) => (s === "sys-a" ? [{ name: "dsp", label: "DSP", assigned: 1, assigned_to: ["dsp-1"], quorum: 1 }] : []));
    mount({ systems: [{ value: "sys-a", label: "Boardroom A" }, { value: "sys-b", label: "Boardroom B" }], members: [member("sys-a", true)] });
    openPanel();
    fireEvent.click(screen.getByRole("radio", { name: "Another system" }));
    await pickInPicker(destination(), "Boardroom B");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => expect(applySteps).toHaveBeenCalled());
    expect(applySteps).toHaveBeenCalledWith("c1", [
      { op: "add", system: "sys-b" },
      { op: "setPrimary", system: "sys-b" },
      { op: "unassign", system: "sys-a", slot: "dsp" },
      { op: "remove", system: "sys-a" },
    ]);
    expect(moveComponent).not.toHaveBeenCalled();
  });

  it("staffs a chosen slot in the destination, and a full slot is not choosable", async () => {
    applySteps.mockResolvedValue(undefined);
    systemSlots.mockImplementation(async (s: string) => (s === "sys-b" ? [
      { name: "mics", label: "Mics", assigned: 1, capacity: 2, quorum: 1, assigned_to: [] },
      { name: "dsp", label: "DSP", assigned: 1, capacity: 1, quorum: 1, assigned_to: ["other"] },
    ] : []));
    const { qc } = mount({ systems: [{ value: "sys-b", label: "Boardroom B" }], members: [member("sys-a", true)] });
    void qc;
    openPanel();
    fireEvent.click(screen.getByRole("radio", { name: "Another system" }));
    await pickInPicker(destination(), "Boardroom B");
    const slot = await screen.findByRole("combobox", { name: "Slot (optional)" });
    fireEvent.input(slot, { target: { value: "" } });
    const options = await screen.findAllByRole("option");
    expect(options.find((o) => o.textContent?.startsWith("DSP"))?.getAttribute("aria-disabled")).toBe("true");
    await pickInPicker(slot, "Mics");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    await waitFor(() => expect(applySteps).toHaveBeenCalled());
    expect((applySteps.mock.calls[0][1] as { op: string; slot?: string }[])[0]).toEqual({ op: "assign", system: "sys-b", slot: "mics" });
  });

  it("shows the server's refusal in place and keeps the panel open", async () => {
    moveSystem.mockRejectedValue({ status: 422, detail: "cannot move a system under itself or a descendant" });
    const { onMoved } = mount({ kind: "system", id: "s1", parents: [{ value: "s2", label: "Child" }] });
    openPanel();
    fireEvent.click(screen.getByRole("radio", { name: "Under a system" }));
    await pickInPicker(destination(), "Child");
    fireEvent.click(screen.getByRole("button", { name: "Move" }));
    const alert = await screen.findByRole("alert");
    expect(alert.textContent).toContain("cannot move a system under itself or a descendant");
    expect(onMoved).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Move" })).toBeTruthy();
  });

  it("does not offer the current place as a destination", async () => {
    mount({ currentPlace: "room-a" });
    openPanel();
    fireEvent.input(destination(), { target: { value: "Boardroom A" } });
    const opt = (await screen.findAllByRole("option"))[0];
    expect(opt.getAttribute("aria-disabled")).toBe("true");
    expect(opt.textContent).toContain("current");
  });

  it("keeps Move disabled until a destination is chosen", () => {
    mount();
    openPanel();
    expect((screen.getByRole("button", { name: "Move" }) as HTMLButtonElement).disabled).toBe(true);
  });
});
