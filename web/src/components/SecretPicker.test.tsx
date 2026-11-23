import { describe, it, expect, afterEach, vi } from "vitest";
import { render, screen, waitFor, fireEvent } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { createSignal } from "solid-js";
import SecretPicker from "./SecretPicker";
import { SECRETS_KEY } from "../lib/secrets";
import { SECRET_TYPES_KEY } from "../lib/secret_types";
import { COMPONENTS_KEY } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import { typeInPicker } from "./ui/Picker.testkit";

// The credential picker of the add-a-component flow (#2460): the secrets of
// the input's kind, chosen by name, and a missing one created in place with
// its kind already set.

const me: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const secrets = [
  { id: uuidFor("s-sim"), name: "sim-community", label: "Sim community", secret_type: "snmp-community", owner_kind: "platform", fields: [] },
  { id: uuidFor("s-login"), name: "kestrel-login", secret_type: "http-basic", owner_kind: "platform", fields: [] },
];
const types = [
  { id: uuidFor("t-api"), name: "api-key", label: "API Key", official: true, fields: [{ name: "key", secret: true }] },
  { id: uuidFor("t-snmp"), name: "snmp-community", label: "SNMP community", official: true, fields: [{ name: "community", secret: true }] },
];

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...SECRETS_KEY], secrets);
  qc.setQueryData([...COMPONENTS_KEY], []);
  const [value, setValue] = createSignal("");
  render(() => (
    <QueryClientProvider client={qc}>
      <label for="community">community</label>
      <SecretPicker id="community" secretType="snmp-community" value={value()} onChange={setValue} />
      <output data-testid="value">{value()}</output>
    </QueryClientProvider>
  ));
  return qc;
}

afterEach(() => vi.restoreAllMocks());

describe("SecretPicker", () => {
  it("offers only the secrets of the input's kind", async () => {
    mount();
    expect(await typeInPicker(screen.getByLabelText("community"), "")).toEqual(["Sim communitysim-community, platform"]);
  });

  it("opens the secret form with the kind set, even when the kinds arrive after it opens", async () => {
    const qc = mount();
    fireEvent.click(screen.getByRole("button", { name: /New snmp-community secret/ }));
    await screen.findByRole("dialog");
    qc.setQueryData([...SECRET_TYPES_KEY], types);
    const kind = await waitFor(() => {
      const sel = [...document.querySelectorAll("select")].find((s) => [...s.options].some((o) => o.value === "snmp-community")) as HTMLSelectElement;
      if (!sel) throw new Error("no type select yet");
      return sel;
    });
    await waitFor(() => expect(kind.value).toBe("snmp-community"));
  });
});
