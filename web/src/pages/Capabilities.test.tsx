import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Capabilities from "./Capabilities";
import { CAPABILITIES_KEY, type Capability } from "../lib/capabilities";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// Capabilities is the capability registry on the flat FlatList surface (the
// capability picker on the product form). Data is seeded into the query cache
// so no server is needed. Registry rows carry a uuid id and the kebab handle
// in name (ADR-0062); the console addresses rows by the handle.
const seed: Capability[] = [
  { id: uuidFor("cap-touch-screen"), name: "touch-screen", display_name: "Touchscreen control", official: true },
  { id: uuidFor("cap-microphone"), name: "microphone", display_name: "Audio capture", official: false },
];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...CAPABILITIES_KEY], seed);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Capabilities />
    </QueryClientProvider>
  ));
}

// The catalog addresses rows by the kebab handle (ADR-0062): the first column
// shows it, and the substring filter matches it. With uuid-shaped fixture ids
// these fail when the page feeds the uuid anywhere an operator reads or types.
describe("Capabilities addressing honesty (#469)", () => {
  afterEach(() => vi.restoreAllMocks());

  it("shows the handle in the Name column and finds the row by it in the filter", async () => {
    mount();
    expect(await screen.findByText("Touchscreen control")).toBeInTheDocument();
    const row = screen.getByText("Touchscreen control").closest("tr")!;
    expect(within(row).getByText("touch-screen")).toBeInTheDocument();
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "touch-screen" } });
    fireEvent.keyDown(input, { key: "Enter" });
    expect(screen.getByText("Touchscreen control")).toBeInTheDocument();
    expect(screen.queryByText("Audio capture")).toBeNull();
  });
});

// The registry reads its identity the way every other list does: one Name column
// carrying the label over the handle (components/IdentityCell), and a create form
// where the handle follows what the operator types (lib/entities).
describe("Capabilities identity", () => {
  afterEach(() => vi.restoreAllMocks());

  it("carries both identities in one Name column, and no retired second column", async () => {
    mount();
    await screen.findByText("Touchscreen control");
    const heads = screen.getAllByRole("columnheader").map((h) => h.textContent?.trim());
    expect(heads).toContain("Name");
    expect(heads.filter((h) => h === "Name")).toHaveLength(1);
    const row = screen.getByText("Touchscreen control").closest("tr")!;
    expect(within(row).getByText("touch-screen")).toBeInTheDocument();
  });

  it("derives the create form's name from the display name until the operator edits it", async () => {
    mount();
    fireEvent.click(screen.getByRole("button", { name: /new capability/i }));
    const display = (await screen.findByPlaceholderText("Microphone")) as HTMLInputElement;
    const name = screen.getByPlaceholderText("microphone") as HTMLInputElement;

    fireEvent.input(display, { target: { value: "Audio Capture" } });
    expect(name.value).toBe("audio-capture");

    // A hand-edited handle is the operator's, so relabelling stops rewriting it.
    fireEvent.input(name, { target: { value: "mic-in" } });
    fireEvent.input(display, { target: { value: "Audio Capture 2" } });
    expect(name.value).toBe("mic-in");
  });
});
