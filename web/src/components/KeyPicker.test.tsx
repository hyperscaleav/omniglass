import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import KeyPicker from "./KeyPicker";
import { KEYS_KEY, type KeyRow } from "../lib/keys";

// KeyPicker is a searchable Kobalte Combobox over the /keys catalog. Its options
// come from the KEYS_KEY query cache (seeded here so no server is needed); the
// dropdown is portaled to document.body, so options are queried with screen.*.
// The combobox opens as the user types (the real search path, and the reliable
// path in jsdom, where the pointer-driven trigger open does not lay out).
const keys: KeyRow[] = [
  { name: "serial_number", data_type: "string", display_name: "Serial number", official: true },
  { name: "mac_address", data_type: "string", display_name: "MAC address", official: true },
  { name: "icmp.reachable", data_type: "int", display_name: "ICMP Reachable", kind: "metric", official: true },
  { name: "rack_unit", data_type: "int", display_name: "Rack unit", official: false },
];

function mount(props: Partial<Parameters<typeof KeyPicker>[0]> = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...KEYS_KEY], keys);
  const onSelect = props.onSelect ?? vi.fn();
  const result = render(() => (
    <QueryClientProvider client={qc}>
      <KeyPicker onSelect={onSelect} {...props} />
    </QueryClientProvider>
  ));
  return { ...result, onSelect };
}

// typeSearch types into the combobox input, opening and filtering the listbox.
async function typeSearch(q: string, expectFirst?: string) {
  const input = screen.getByRole("combobox");
  fireEvent.input(input, { target: { value: q } });
  if (expectFirst) await waitFor(() => expect(screen.getByText(expectFirst)).toBeTruthy());
}

describe("KeyPicker", () => {
  afterEach(() => vi.restoreAllMocks());

  it("renders a searchable combobox input", () => {
    mount();
    expect(screen.getByRole("combobox")).toBeTruthy();
  });

  it("surfaces matching keys as the operator types", async () => {
    mount();
    await typeSearch("serial", "serial_number");
    // The typed query narrows to the match; a non-matching key is not offered.
    expect(screen.queryByText("mac_address")).toBeNull();
  });

  it("matches on the human label, not only the raw key", async () => {
    mount();
    // "MAC address" is mac_address's label; searching it finds the key.
    await typeSearch("MAC address", "mac_address");
  });

  it("narrows the catalog with the filter prop", async () => {
    // Declared-only keys (no observed kind) is the field-editor lens: an observed
    // (metric) key is dropped even when the typed query would match its text.
    mount({ filter: (k) => !k.kind });
    await typeSearch("a", "rack_unit");
    expect(screen.queryByText("icmp.reachable")).toBeNull();
  });

  it("omits keys in the exclude set", async () => {
    mount({ exclude: ["serial_number"] });
    // "a" matches both serial_number and mac_address by text; the excluded one is gone.
    await typeSearch("a", "mac_address");
    expect(screen.queryByText("serial_number")).toBeNull();
  });

  it("calls onSelect with the picked key (its data_type rides along)", async () => {
    const onSelect = vi.fn();
    mount({ onSelect });
    await typeSearch("serial", "serial_number");
    fireEvent.click(screen.getByText("serial_number"));
    await waitFor(() => expect(onSelect).toHaveBeenCalled());
    const picked = onSelect.mock.calls.at(-1)?.[0] as KeyRow;
    expect(picked?.name).toBe("serial_number");
    expect(picked?.data_type).toBe("string");
  });

  it("portals the dropdown outside the picker's own container", async () => {
    const { container } = mount();
    await typeSearch("serial", "serial_number");
    // The option is mounted to the body portal, not inside the render container.
    expect(within(container).queryByText("serial_number")).toBeNull();
    expect(screen.getByText("serial_number")).toBeTruthy();
  });

  it("disables the input when disabled", () => {
    mount({ disabled: true });
    expect((screen.getByRole("combobox") as HTMLInputElement).disabled).toBe(true);
  });
});
