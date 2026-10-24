import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { createSignal } from "solid-js";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SessionNotice from "./SessionNotice";
import { ME_KEY, type Me } from "../lib/auth";
import { REACHABILITY_KEY, type ReachEndpoint } from "../lib/reachability";
import { ENDPOINTS_KEY } from "../lib/endpoints";
import { uuidFor } from "../lib/testids";

// A changed key, trusted from the console (#1078). A session rejected for a
// changed ssh host key names both keys, read off the gate as data (never out
// of the reason), and offers Trust this key behind endpoint:retry. Trusting is
// a security decision, so it confirms first, teaching why a key changes; the
// confirm sends exactly the key it showed, and a key that changed while it was
// open is never sent. Data is seeded into the query cache, the house shape.

const now = Date.now();
const ago = (ms: number) => new Date(now - ms).toISOString();
const compId = uuidFor("comp-ssh-panel");
const epId = uuidFor("ep-ssh");
const pinned = "SHA256:" + "P".repeat(43);
const offered = "SHA256:" + "O".repeat(43);
const offeredAgain = "SHA256:" + "Q".repeat(43);

const hostKeyRejected = (over: Partial<ReachEndpoint> = {}): ReachEndpoint => ({
  id: epId,
  endpoint: "ssh",
  label: "Panel SSH",
  transport: "ssh",
  node: "edge-hq",
  last_seen: ago(2_000),
  verdict: { value: "up", ts: ago(60_000) },
  responsive: { value: "up", ts: ago(60_000) },
  // The reason names other keys: a notice that parsed the prose would show them.
  session: { state: "rejected", ts: ago(30_000), reason: `host-key: the host key changed: pinned SHA256:${"x".repeat(43)}, offered SHA256:${"y".repeat(43)}` },
  rejection: { rung: "responsive", class: "host-key", ts: "2026-10-23T11:58:00.000Z", pinned_key: pinned, offered_key: offered },
  layers: [{ layer: "ping", check: "icmp-reachable", value: 1, ts: ago(2_000) }],
  history: [],
  ...over,
});

const operator: Me = { principal: { id: "p1", kind: "human" }, permissions: ["*:read", "endpoint:update", "endpoint:retry"], grants: [] };
const viewer: Me = { principal: { id: "p2", kind: "human" }, permissions: ["*:read"], grants: [] };

const ok = () =>
  new Response(JSON.stringify({ id: epId, name: "ssh", transport: "ssh" }), { status: 200, headers: { "Content-Type": "application/json" } });

function mount(start: ReachEndpoint, me: Me | null = operator) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  qc.setQueryData([...REACHABILITY_KEY(compId)], { component: "panel", endpoints: [start] });
  qc.setQueryData([...ENDPOINTS_KEY], []);
  const invalidate = vi.spyOn(qc, "invalidateQueries");
  const [iface, setIface] = createSignal(start);
  render(() => (
    <QueryClientProvider client={qc}>
      <SessionNotice iface={iface()} component={compId} now={now} />
    </QueryClientProvider>
  ));
  return { setIface, invalidate };
}

const trustButton = () => screen.getByRole("button", { name: "Trust this key" }) as HTMLButtonElement;
const dialog = () => screen.getByRole("alertdialog");

describe("PinnedKeyNotice", () => {
  beforeEach(() => vi.restoreAllMocks());

  it("names the pinned and the offered key from the gate, never from the reason", () => {
    mount(hostKeyRejected());
    const notice = screen.getByTestId("pinned-key-notice");
    expect(screen.getByTestId("pinned-key").textContent).toBe(pinned);
    expect(screen.getByTestId("offered-key").textContent).toBe(offered);
    expect(notice.textContent).not.toContain("x".repeat(43));
    expect(screen.getByText("The host key changed")).toBeTruthy();
  });

  it("teaches each key on its label", async () => {
    mount(hostKeyRejected());
    const tip = screen.getByRole("button", { name: "More about Offered host key" });
    fireEvent.keyDown(document.body, { key: "Tab" });
    tip.focus();
    expect(await screen.findByText(/presented instead/i)).toBeTruthy();
  });

  it("confirms before trusting, teaching why a key changes, and Cancel sends nothing", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    mount(hostKeyRejected());
    fireEvent.click(trustButton());
    const d = dialog();
    expect(d.textContent).toContain(pinned);
    expect(d.textContent).toContain(offered);
    expect(d.textContent).toMatch(/Panel SSH/);
    // Each cause is a chip that teaches itself.
    const mitm = screen.getByRole("button", { name: "Man in the middle" });
    fireEvent.keyDown(document.body, { key: "Tab" });
    mitm.focus();
    expect(await screen.findByText(/answering in its place/i)).toBeTruthy();
    fireEvent.click(screen.getByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("trusts exactly the offered key it showed, then waits on the session", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(ok());
    const { invalidate } = mount(hostKeyRejected());
    fireEvent.click(trustButton());
    fireEvent.click(screen.getByRole("button", { name: "Trust this key" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    const req = fetchMock.mock.calls[0][0] as Request;
    expect(req.method).toBe("POST");
    expect(req.url).toContain(`/endpoints/${epId}:retry`);
    expect(await req.json()).toEqual({ host_key: offered });
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: [...REACHABILITY_KEY(compId)] }));
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
    // Until the session moves, neither Trust nor Retry can stack a second redial.
    await waitFor(() => expect(trustButton().disabled).toBe(true));
    expect((screen.getByRole("button", { name: /retry/i }) as HTMLButtonElement).disabled).toBe(true);
  });

  // The stale offered key: the component's key changed again while the
  // confirm was open. The confirm keeps showing the key it opened with, says
  // the offer moved, and will not send a key the operator did not read.
  it("never sends a key that changed while the confirm was open", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(ok());
    const { setIface } = mount(hostKeyRejected());
    fireEvent.click(trustButton());
    setIface(hostKeyRejected({ rejection: { rung: "responsive", class: "host-key", ts: "2026-10-23T11:58:00.000Z", pinned_key: pinned, offered_key: offeredAgain } }));
    const d = dialog();
    await waitFor(() => expect(screen.getByRole("alert").textContent).toMatch(/offers a different key/i));
    expect(d.textContent).toContain(offered);
    const confirm = screen.getByRole("button", { name: "Trust this key" }) as HTMLButtonElement;
    expect(confirm.disabled).toBe(true);
    fireEvent.click(confirm);
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("says why the server refused a trust, in the confirm", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(JSON.stringify({ title: "Conflict", status: 409, detail: `the component now offers ${offeredAgain}, not the key sent` }), {
        status: 409,
        headers: { "Content-Type": "application/problem+json" },
      }),
    );
    mount(hostKeyRejected());
    fireEvent.click(trustButton());
    fireEvent.click(screen.getByRole("button", { name: "Trust this key" }));
    expect((await screen.findByRole("alert")).textContent).toContain(offeredAgain);
    expect(screen.getByRole("alertdialog")).toBeTruthy();
  });

  it("greys Trust with its reason for a caller without endpoint:retry, and never opens or calls", async () => {
    const fetchMock = vi.spyOn(globalThis, "fetch");
    mount(hostKeyRejected(), viewer);
    expect(trustButton().disabled).toBe(true);
    expect(screen.getByLabelText(/trusting a key needs the endpoint:retry permission/i)).toBeTruthy();
    fireEvent.click(trustButton());
    expect(screen.queryByRole("alertdialog")).toBeNull();
    await new Promise((r) => setTimeout(r, 20));
    expect(fetchMock).not.toHaveBeenCalled();
    // The keys are still read: a viewer can tell the operator what changed.
    expect(screen.getByTestId("offered-key").textContent).toBe(offered);
  });

  it("offers no trust for a refusal that is not about a key", () => {
    mount(hostKeyRejected({ session: { state: "rejected", ts: ago(30_000), reason: "auth: auth rejected" }, rejection: { rung: "authenticated", class: "auth", ts: "2026-10-23T11:58:00.000Z" } }));
    expect(screen.queryByTestId("pinned-key-notice")).toBeNull();
    expect(screen.queryByRole("button", { name: "Trust this key" })).toBeNull();
    expect(screen.getByTestId("session-reason").textContent).toBe("auth: auth rejected");
  });
});

// #1110: a changed key the reachability probe met, on an endpoint holding no
// session (an ssh probe's host key, a pinned tls certificate's key), reads the
// same: both keys from the gate, Trust this key behind endpoint:retry, and the
// trust waits on the gate rather than a session there is none of.
describe("PinnedKeyNotice for a probe's gate, no session held", () => {
  beforeEach(() => vi.restoreAllMocks());

  it.each([
    ["ssh", "host-key", "The host key changed"],
    ["tls", "certificate", "The certificate key changed"],
  ] as const)("names both keys of a %s probe's refusal and trusts the offered one", async (transport, cls, heading) => {
    const fetchMock = vi.spyOn(globalThis, "fetch").mockResolvedValue(ok());
    const probe = hostKeyRejected({
      endpoint: transport,
      transport,
      session: undefined,
      rejection: { rung: "responsive", class: cls, ts: ago(40_000), pinned_key: pinned, offered_key: offered, reason: `${cls}: the key changed` },
    });
    const { invalidate } = mount(probe);
    expect(screen.getByText(heading)).toBeTruthy();
    expect(screen.getByTestId("pinned-key").textContent).toBe(pinned);
    expect(screen.getByTestId("offered-key").textContent).toBe(offered);
    expect(screen.getByRole("button", { name: /retry/i })).toBeTruthy();
    fireEvent.click(trustButton());
    fireEvent.click(screen.getByRole("button", { name: "Trust this key" }));
    await waitFor(() => expect(fetchMock).toHaveBeenCalledOnce());
    expect(await (fetchMock.mock.calls[0][0] as Request).json()).toEqual({ host_key: offered });
    await waitFor(() => expect(invalidate).toHaveBeenCalledWith({ queryKey: [...REACHABILITY_KEY(compId)] }));
    await waitFor(() => expect(trustButton().disabled).toBe(true));
  });

  it("greys Trust for a caller without endpoint:retry, the keys still read", () => {
    mount(hostKeyRejected({ session: undefined, rejection: { rung: "responsive", class: "host-key", ts: ago(40_000), pinned_key: pinned, offered_key: offered } }), viewer);
    expect(trustButton().disabled).toBe(true);
    expect(screen.getByTestId("offered-key").textContent).toBe(offered);
  });
});
