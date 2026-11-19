import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import Audit from "./Audit";
import { AUDIT_PAGE, AUDIT_LOADING_ROWS, type AuditEvent } from "../lib/audit";
import { uuidFor } from "../lib/testids";

// The Audit page renders the trail newest-first (resolving the actor to a name and
// marking an impersonated action), and wears the list-view standard: the shared
// FilterBar narrows the loaded rows, and "load older" pages back via the server
// `before` cursor. The network is the seam we fake (fetch), so the page is driven
// exactly as a user would without a server.
const seed: AuditEvent[] = [
  { id: "1", ts: "2026-07-07T10:02:00Z", actor: "u-alice", actor_name: "alice", verb: "login", resource: "auth" },
  { id: "2", ts: "2026-07-07T10:01:00Z", actor: "u-alice", actor_name: "alice", real_actor: "u-root", real_actor_name: "root", verb: "update", resource: "user", resource_id: "u-alice", old: { label: "Alice" }, new: { label: "Alice Cooper" } },
  { id: uuidFor("3"), ts: "2026-07-07T10:00:00Z", actor: "u-bob", actor_name: "bob", verb: "delete", resource: "component", resource_id: "cmp_9f2", old: { name: "encoder" } },
  { id: uuidFor("4"), ts: "2026-07-07T09:59:00Z", actor: "u-bob", actor_name: "bob", verb: "create", resource: "secret", resource_id: "00000000-0000-4000-8000-00000000aaaa", new: { name: "core-snmp", secret_type: "snmp-community" } },
  { id: uuidFor("5"), ts: "2026-07-07T09:58:00Z", actor: "u-bob", actor_name: "bob", verb: "set", resource: "tag_binding", resource_id: "hq" },
  { id: uuidFor("6"), ts: "2026-07-07T09:57:00Z", actor: "u-bob", actor_name: "bob", verb: "somenewverb", resource: "widget", resource_id: "w-1" },
  // An endpoint retry that trusted a host key (#1088): its jsonb columns
  // arrive as the objects they are, the values long enough to need wrapping.
  {
    id: uuidFor("7"), ts: "2026-07-07T09:56:00Z", actor: "u-dev", actor_name: "dev", verb: "retry", resource: "endpoint",
    resource_id: "00000000-0000-4000-8000-00000000eeee",
    old: { Name: "ssh", Params: { target: "127.60.0.19:22", timeout: "5s" }, Inputs: { host: "127.60.0.19", login: "touch-panel-login" } },
    new: {
      Name: "ssh", host_key: "SHA256:7yS0pJqk0m3wQ2v9W6dXn1bT8cHf4LrZ5aEoGuKiMNs",
      Params: { target: "127.60.0.19:22", timeout: "5s" }, Inputs: { host: "127.60.0.19", login: "touch-panel-login" },
    },
  },
  // An agent's change through the MCP server (#1178): the channel is server-
  // stamped, the client is what the MCP client declared, and the agent is the
  // name its agent token was minted with.
  {
    id: uuidFor("8"), ts: "2026-07-07T09:55:00Z", actor: "u-carol", actor_name: "carol", verb: "disable", resource: "component",
    resource_id: "cmp_amp", new: { name: "rack-amp" }, channel: "mcp", client: "claude-code/2.1.0", agent: "claude-desktop",
  },
];

const beforeParams: (string | null)[] = [];

beforeEach(() => {
  vi.restoreAllMocks();
  beforeParams.length = 0;
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = typeof input === "string" ? input : (input as Request).url;
    const before = new URL(url, "http://local").searchParams.get("before");
    beforeParams.push(before);
    // A page older than the newest page is empty (nothing further back).
    const events = before ? [] : seed;
    return new Response(JSON.stringify({ events }), { status: 200, headers: { "Content-Type": "application/json" } });
  });
});

describe("Audit page", () => {
  // The first read holds the table's space with skeleton rows (and the footer's
  // button keeps one label width), so the loaded rows land in place instead of
  // shifting the page (the probe's layout-shift budget, #1461).
  it("reserves the list's height with skeleton rows while the first page loads", async () => {
    let release: () => void = () => {};
    const gate = new Promise<void>((r) => (release = r));
    vi.spyOn(globalThis, "fetch").mockImplementation(async () => {
      await gate;
      return new Response(JSON.stringify({ events: seed }), { status: 200, headers: { "Content-Type": "application/json" } });
    });
    render(() => <Audit />);
    expect(screen.getAllByTestId("list-loading-row")).toHaveLength(AUDIT_LOADING_ROWS);
    expect(screen.getByText("Load older")).toBeTruthy();
    release();
    expect(await screen.findByText("login")).toBeTruthy();
    expect(screen.queryAllByTestId("list-loading-row")).toHaveLength(0);
  });

  // A first page longer than the skeleton must not push the footer down the
  // page (#1461): the rows scroll inside the card, the footer stays below them.
  it("scrolls the rows inside the list, the footer pinned outside that region", async () => {
    render(() => <Audit />);
    const table = (await screen.findByText("login")).closest("table")!;
    const region = table.parentElement!;
    expect(region.classList.contains("overflow-y-auto")).toBe(true);
    expect(region.contains(screen.getByText("No older events"))).toBe(false);
    // The document scrolls, not the shell, so the list only fills a box with a
    // definite height: without one it grows with its rows and the footer moves.
    const shell = table.closest(".og-stack")!;
    expect(shell.parentElement!.className).toMatch(/\bh-\[calc\(100dvh/);
  });

  it("renders events with actor, verb, and resource", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    expect(await findByText("login")).toBeTruthy();
    expect(getByText("auth")).toBeTruthy();
    expect(getByText("delete")).toBeTruthy();
  });

  it("shows the impersonator as the actor, tagged with the identity they assumed", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    // Row 2 was root acting as alice: "root" (the real human) is the accountable
    // who, with an "as alice" tag naming whose identity was assumed.
    expect(await findByText(/as alice/)).toBeTruthy();
    expect(getByText("root")).toBeTruthy();
  });

  it("narrows the loaded rows through the FilterBar (a bare term filters the actor)", async () => {
    const { findByText, getByRole, queryByText, getAllByText } = render(() => <Audit />);
    await findByText("login");
    const input = getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: "alice" } });
    fireEvent.keyDown(input, { key: "Enter" });
    // Only the two alice rows remain; bob's row is filtered out.
    expect(getAllByText("alice").length).toBeGreaterThan(0);
    expect(queryByText("bob")).toBeNull();
    expect(queryByText("delete")).toBeNull();
  });

  it("pages older through the server `before` cursor", async () => {
    // A full first page (== AUDIT_PAGE) means there may be more, so load-older is
    // live; the oldest row carries a known timestamp the cursor must use.
    const oldestTs = "2026-07-07T09:00:00Z";
    const full: AuditEvent[] = Array.from({ length: AUDIT_PAGE }, (_, i) => ({
      id: `f${i}`,
      ts: i === AUDIT_PAGE - 1 ? oldestTs : "2026-07-07T10:00:00Z",
      actor_name: "alice", verb: "login", resource: "auth",
    }));
    vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
      const url = typeof input === "string" ? input : (input as Request).url;
      const before = new URL(url, "http://local").searchParams.get("before");
      beforeParams.push(before);
      return new Response(JSON.stringify({ events: before ? [] : full }), { status: 200, headers: { "Content-Type": "application/json" } });
    });

    const { findByText, getByText } = render(() => <Audit />);
    await findByText("Load older");
    // Re-query each poll: the footer re-renders (replacing the button outpost) as the
    // loaded-count updates, so a captured reference would go stale. Wait for the
    // first load to finish so the button is live (not the no-op it is in flight).
    const btn = () => getByText("Load older") as HTMLButtonElement;
    await waitFor(() => expect(btn().disabled).toBe(false));
    expect(beforeParams).toEqual([null]); // first page has no cursor
    fireEvent.click(btn());
    // The load-older page asks for events strictly older than the oldest loaded row.
    await waitFor(() => expect(beforeParams).toContain(oldestTs));
  });
});

// The drawer answers the second half of "who changed this, and to what?"
// (#473): a row opens into the field-level before/after diff over the images
// the write recorded.
describe("Audit drawer (#473)", () => {
  it("opens a row into a drawer with the field-level before/after diff", async () => {
    const { findByText } = render(() => <Audit />);
    // The drawer portals to document.body, so the assertions use screen.
    fireEvent.click(await findByText("update"));
    expect(await screen.findByText("Change")).toBeTruthy();
    expect(screen.getByText("label")).toBeTruthy();
    expect(screen.getByText("Alice")).toBeTruthy();
    expect(screen.getByText("Alice Cooper")).toBeTruthy();
  });

  it("a delete renders only its Before column: there is no After image to give room to", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("delete"));
    const dialog = await screen.findByRole("dialog");
    expect(screen.getAllByText("encoder").length).toBeGreaterThan(0);
    expect([...dialog.querySelectorAll("th")].map((th) => th.textContent)).toEqual(["Field", "Before"]);
    expect(dialog.querySelectorAll("col")).toHaveLength(2);
    expect(screen.queryByText("(none)")).toBeNull();
  });

  it("a create renders only its After column (#1088)", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("core-snmp"));
    const dialog = await screen.findByRole("dialog");
    expect([...dialog.querySelectorAll("th")].map((th) => th.textContent)).toEqual(["Field", "After"]);
    const row = [...dialog.querySelectorAll("tr")].find((tr) => tr.firstElementChild?.textContent === "secret_type")!;
    expect([...row.children].map((td) => td.textContent)).toEqual(["secret_type", "snmp-community"]);
  });

  it("an update with a field only one side carries still reads (none) on the other", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("retry"));
    const dialog = await screen.findByRole("dialog");
    const row = [...dialog.querySelectorAll("tr")].find((tr) => tr.firstElementChild?.textContent === "host_key")!;
    expect(row.children[1].textContent).toBe("(none)");
  });

  it("an event that recorded no images says so instead of an empty table", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("login"));
    expect(await screen.findByText(/recorded no row images/)).toBeTruthy();
  });
});

// The trail stores the uuid as resource_id (identity survives a rename), but an
// operator reads the friendly handle (ADR-0062): the console resolves it from
// the row's own images, keeping the uuid one hover away.
describe("Audit resource labels (ADR-0062)", () => {
  it("shows the handle from the row's image in the Id column, uuid on hover", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    expect(await findByText("core-snmp")).toBeTruthy();
    expect(getByText("core-snmp").getAttribute("title")).toContain("00000000-0000-4000-8000-00000000aaaa");
  });

  it("falls back to the raw resource_id when no image carries a name", async () => {
    const { findByText } = render(() => <Audit />);
    // Row 2 (user update) images carry no name key; its id renders as-is.
    expect(await findByText("u-alice")).toBeTruthy();
  });

  it("titles the drawer with the handle, not the uuid", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("core-snmp"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("create secret core-snmp");
    expect(dialog.textContent).not.toContain("create secret 00000000-0000-4000");
  });
});

// Every action badge wears the same soft treatment: known verbs take their
// semantic hue and an unmapped verb falls back to a soft neutral, never a
// second visual style (the plain ghost badge rendered set/enroll as outlined
// boxes beside the tinted pills).
describe("Audit action badge consistency", () => {
  it("renders every verb, mapped or not, with the soft badge treatment", async () => {
    const { findByText, getByText } = render(() => <Audit />);
    await findByText("login");
    for (const verb of ["login", "update", "delete", "create", "set", "somenewverb"]) {
      const badge = getByText(verb, { selector: ".badge" });
      expect(badge.className, `verb ${verb}`).toContain("badge-soft");
    }
  });
});

// An endpoint's audit row reads as JSON and shows its whole change (#1088):
// a jsonb column renders as indented JSON, never base64, and the change table
// is laid out to fit the drawer, so the After column is never pushed off it.
describe("Audit drawer fits a jsonb change (#1088)", () => {
  it("renders an endpoint's params and inputs as indented JSON", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("retry"));
    const dialog = await screen.findByRole("dialog");
    const row = (field: string) => [...dialog.querySelectorAll("tr")].find((tr) => tr.firstElementChild?.textContent === field)!;
    const params = row("Params").children;
    expect(params[1].textContent).toBe('{\n  "target": "127.60.0.19:22",\n  "timeout": "5s"\n}');
    expect(params[2].textContent).toBe(params[1].textContent);
    expect(row("Inputs").children[2].textContent).toContain('"login": "touch-panel-login"');
  });

  it("fixes the trail's column widths so masked cell text cannot size them", async () => {
    const { container, findByText } = render(() => <Audit />);
    await findByText("retry");
    const table = container.querySelector("table")!;
    expect(table.className).toContain("table-fixed");
    expect(table.className).toContain("w-full");
  });

  it("lays the change table out to the drawer's width, its values wrapping in place", async () => {
    const { findByText } = render(() => <Audit />);
    fireEvent.click(await findByText("retry"));
    const dialog = await screen.findByRole("dialog");
    // The wide drawer, the one a body that reads more than a form takes.
    expect(dialog.className).toContain("max-w-2xl");
    const table = dialog.querySelector("table")!;
    expect(table.className).toContain("table-fixed");
    expect(table.className).toContain("w-full");
    expect([...dialog.querySelectorAll("th")].map((th) => th.textContent)).toEqual(["Field", "Before", "After"]);
    const added = [...dialog.querySelectorAll("tr")].find((tr) => tr.firstElementChild?.textContent === "host_key")!;
    for (const cell of [...added.children].slice(1)) {
      expect(cell.className).toContain("whitespace-pre-wrap");
      expect(cell.className).toContain("wrap-break-word");
    }
    expect(added.children[2].textContent).toBe("SHA256:7yS0pJqk0m3wQ2v9W6dXn1bT8cHf4LrZ5aEoGuKiMNs");
  });
});

// How a change arrived (#1178): a row that came over the MCP server wears a
// channel pill with the client it declared beneath; a direct write wears none.
// The agent that acted rides beside the person it acted for.
describe("Audit channel and client (#1178)", () => {
  it("marks an MCP write with a channel pill, the declared client as subtext", async () => {
    render(() => <Audit />);
    const pill = await screen.findByText("mcp", { selector: ".badge" });
    expect(pill.className).toContain("badge-soft");
    const cell = pill.closest("td")!;
    expect(cell.textContent).toContain("claude-code/2.1.0");
    const row = pill.closest("tr")!;
    expect(row.textContent).toContain("carol");
    expect(row.textContent).toContain("claude-desktop");
  });

  it("leaves a direct write without a pill", async () => {
    render(() => <Audit />);
    await screen.findByText("login");
    // Eight rows, one of them over MCP: exactly one pill.
    expect(screen.getAllByTestId("audit-channel")).toHaveLength(1);
    const loginRow = screen.getByText("login").closest("tr")!;
    expect(loginRow.querySelector('[data-testid="audit-channel"]')).toBeNull();
  });

  const typeChip = (token: string) => {
    const input = screen.getByRole("combobox") as HTMLInputElement;
    fireEvent.input(input, { target: { value: token } });
    fireEvent.keyDown(input, { key: "Enter" });
  };

  it("narrows to MCP writes with the channel filter, and to direct ones with channel:direct", async () => {
    render(() => <Audit />);
    await screen.findByText("login");
    typeChip("channel:mcp");
    expect(screen.getByText("rack-amp")).toBeTruthy();
    expect(screen.queryByText("login")).toBeNull();
    expect(screen.queryByText("core-snmp")).toBeNull();
  });

  it("offers direct as a channel, matching the rows with none", async () => {
    render(() => <Audit />);
    await screen.findByText("login");
    typeChip("channel:direct");
    expect(screen.getByText("login")).toBeTruthy();
    expect(screen.queryByText("rack-amp")).toBeNull();
  });

  it("narrows to one client with the client search", async () => {
    render(() => <Audit />);
    await screen.findByText("login");
    typeChip("client:claude");
    expect(screen.getByText("rack-amp")).toBeTruthy();
    expect(screen.queryByText("login")).toBeNull();
  });

  it("names the channel, the agent and the client in the drawer, the client as the agent reported it", async () => {
    render(() => <Audit />);
    fireEvent.click(await screen.findByText("rack-amp"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("Channel");
    expect(dialog.textContent).toContain("Client");
    expect(dialog.textContent).toContain("as the agent reported it");
    expect(dialog.textContent).toContain("claude-code/2.1.0");
    expect(dialog.textContent).toContain("claude-desktop");
    // The channel teaches what it proves and what it does not.
    expect(screen.getByLabelText("More about Channel")).toBeTruthy();
  });

  it("names a direct write's channel as direct in the drawer, with no client", async () => {
    render(() => <Audit />);
    fireEvent.click(await screen.findByText("delete"));
    const dialog = await screen.findByRole("dialog");
    expect(dialog.textContent).toContain("direct");
    expect(dialog.textContent).not.toContain("as the agent reported it");
  });
});
