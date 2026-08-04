import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import CommandTypes from "./CommandTypes";
import { COMMAND_TYPES_KEY, type CommandTypeRow } from "../lib/command_types";
import { ME_KEY, type Me } from "../lib/auth";

// The Command Types page is a single FlatList over the /command-types catalog.
// Official types are read-only; a custom type is writable only when the caller holds
// command_type:create / command_type:update. Data is seeded into the query cache.
const seed: CommandTypeRow[] = [
  { name: "set-input", display_name: "Set input", target_property_type: "video.input", settle_window_seconds: 15, official: true },
  { name: "reboot", display_name: "Reboot", settle_window_seconds: 0, official: true },
  { name: "set-volume", display_name: "Set volume", target_property_type: "audio.level", settle_window_seconds: 5, official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...COMMAND_TYPES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <CommandTypes />
    </QueryClientProvider>
  ));
}

describe("Command Types page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the seeded command types with their target and settle window", () => {
    mount();
    expect(screen.getByText("set-input")).toBeTruthy();
    expect(screen.getByText("reboot")).toBeTruthy();
    expect(screen.getByText("video.input")).toBeTruthy();
    // reboot is fire-and-forget (no target).
    expect(screen.getByText("fire-and-forget")).toBeTruthy();
  });

  // One identity column carries both operator-facing identities, so the separate
  // label column is gone. The header is the one word every list uses, even though
  // this catalog's names may be dotted (set-input, icmp.rtt-avg) rather than kebab.
  it("carries the display name and the name in a single Name column", () => {
    mount();
    const headers = screen.getAllByRole("columnheader").map((h) => h.textContent?.trim());
    expect(headers).toContain("Name");
    expect(headers).not.toContain("Label");
    expect(screen.getByText("Set input")).toBeTruthy();
    expect(screen.getByText("set-input")).toBeTruthy();
  });

  it("shows New command type for a caller holding command_type:create", () => {
    mount(admin);
    expect(screen.getByText("New command type")).toBeTruthy();
  });

  it("hides New command type from a read-only viewer", () => {
    mount(viewer);
    expect(screen.queryByText("New command type")).toBeNull();
  });
});
