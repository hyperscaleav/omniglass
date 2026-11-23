import { uuidFor } from "../lib/testids";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createSignal } from "solid-js";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { MemoryRouter, Route, useLocation } from "@solidjs/router";
import CommandPalette from "./CommandPalette";
import { ME_KEY, type Me } from "../lib/auth";
import type { SearchResult } from "../lib/palette";

// The palette finds things, not just pages (#2444): what is typed goes to the
// server (debounced, the last request cancelled by the next keystroke), and what
// comes back is laid out in groups, each component with its location, the first row
// the default, "Ask omnibot" always last.
const HITS: SearchResult = {
  locations: [{ id: uuidFor("loc-aud"), name: "auditorium", location_path: "hq" }],
  systems: [{ id: uuidFor("sys-1"), name: "auditorium-av", location_path: "hq / auditorium" }],
  components: [
    { id: uuidFor("c-1"), name: "panel-1", location_path: "hq / auditorium", system: "auditorium-av" },
    { id: uuidFor("c-2"), name: "panel-1", location_path: "hq / boardroom" },
  ],
  catalog: [{ kind: "driver", name: "boreal-edge-snmp", label: "Boreal Edge SNMP", detail: "1.0.0" }],
};
const EMPTY: SearchResult = { locations: [], systems: [], components: [], catalog: [] };

const ME: Me = { user: { id: uuidFor("u-1"), kind: "person" }, person: { username: "op" }, permissions: [">", "assistant:use"], grants: [] };

type Call = { q: string; signal: AbortSignal };
let calls: Call[];
let answer: (q: string) => SearchResult | Promise<SearchResult>;

function Where() {
  const l = useLocation();
  return <span data-testid="where">{l.pathname + l.search}</span>;
}

function mount(opts: { onAsk?: (p: string) => void } = {}) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], ME);
  const [open, setOpen] = createSignal(false);
  const onClose = vi.fn(() => setOpen(false));
  render(() => (
    <QueryClientProvider client={qc}>
      <MemoryRouter
        root={(p) => (
          <>
            <button data-testid="trigger" onClick={() => setOpen(true)}>
              open
            </button>
            <Where />
            <CommandPalette open={open()} onClose={onClose} onAsk={opts.onAsk ?? (() => {})} />
            {p.children}
          </>
        )}
      >
        <Route path="*" component={() => null} />
      </MemoryRouter>
    </QueryClientProvider>
  ));
  const trigger = screen.getByTestId("trigger");
  trigger.focus();
  fireEvent.click(trigger);
  return { onClose, trigger };
}

const field = () => screen.findByRole("combobox", { name: "Search the console" });
const type = async (text: string) => {
  const f = await field();
  f.focus();
  fireEvent.input(f, { target: { value: text } });
  // Kobalte Search reports the input on a zero-delay timer.
  await vi.advanceTimersByTimeAsync(10);
  return f;
};
const rowTexts = async () => within(await screen.findByRole("listbox")).getAllByRole("option").map((o) => o.textContent);
const groupHeadings = (list: HTMLElement) => [...list.querySelectorAll("[data-group-start]")].map((e) => e.getAttribute("data-group-start"));
const settle = () => vi.advanceTimersByTimeAsync(250);

beforeEach(() => {
  calls = [];
  answer = () => HITS;
  sessionStorage.clear();
  vi.useFakeTimers({ shouldAdvanceTime: true });
  vi.stubGlobal(
    "fetch",
    vi.fn(async (req: Request) => {
      const url = new URL(req.url);
      if (url.pathname.endsWith("/search")) {
        const q = url.searchParams.get("q") ?? "";
        calls.push({ q, signal: req.signal });
        const body = await answer(q);
        return new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
      }
      return new Response("{}", { status: 200, headers: { "Content-Type": "application/json" } });
    }),
  );
});
afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

describe("palette results", () => {
  it("lays what the server found out in groups, each component with its location, Ask omnibot last", async () => {
    mount();
    await type("panel");
    await settle();
    await waitFor(async () => expect((await rowTexts()).some((t) => t?.includes("panel-1"))).toBe(true));
    const list = await screen.findByRole("listbox");
    expect(groupHeadings(list)).toEqual(["Locations", "Systems", "Components", "Catalog", "Actions"]);
    const texts = await rowTexts();
    expect(texts).toContain("Components: panel-1hq / auditorium · in auditorium-av");
    expect(texts).toContain("Components: panel-1hq / boardroom");
    expect(texts).toContain("Catalog: Boreal Edge SNMPDriver · 1.0.0");
    expect(texts.at(-1)).toBe("Actions: Ask omnibot: panel");
  });

  it("makes the first match the default, never Ask omnibot, and Enter opens it", async () => {
    mount();
    const f = await type("auditorium");
    await settle();
    await waitFor(async () => expect((await rowTexts()).length).toBeGreaterThan(2));
    expect(document.querySelector("[data-first]")?.textContent).toContain("auditorium");
    expect(document.querySelector("[data-first]")?.textContent).not.toContain("Ask omnibot");
    fireEvent.keyDown(f, { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe(`/locations/${uuidFor("loc-aud")}`));
  });

  it("is the Ask row alone when nothing matched, and Enter hands the query to omnibot", async () => {
    answer = () => EMPTY;
    const onAsk = vi.fn();
    mount({ onAsk });
    const f = await type("zzzzqq");
    await settle();
    await waitFor(async () => expect(await rowTexts()).toEqual(["Actions: Ask omnibot: zzzzqq"]));
    fireEvent.keyDown(f, { key: "Enter" });
    expect(onAsk).toHaveBeenCalledWith("zzzzqq");
  });

  it("moves with the arrows and opens a component at its page", async () => {
    mount();
    const f = await type("panel");
    await settle();
    await waitFor(async () => expect((await rowTexts()).length).toBeGreaterThan(4));
    fireEvent.keyDown(f, { key: "ArrowDown" });
    fireEvent.keyDown(f, { key: "ArrowDown" });
    fireEvent.keyDown(f, { key: "ArrowDown" });
    await waitFor(() => expect(document.querySelector("[role=option][data-highlighted]")?.textContent).toContain("panel-1hq / auditorium"));
    fireEvent.keyDown(f, { key: "Enter" });
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe(`/components/${uuidFor("c-1")}`));
  });

  it("opens a catalog entry at its drawer address", async () => {
    mount();
    await type("boreal");
    await settle();
    const row = await screen.findByRole("option", { name: /Boreal Edge SNMP/ });
    fireEvent.click(row);
    await waitFor(() => expect(screen.getByTestId("where").textContent).toBe("/drivers?open=boreal-edge-snmp"));
  });
});

describe("palette search requests", () => {
  it("waits for a pause and cancels the request a keystroke overtakes", async () => {
    let release!: (r: SearchResult) => void;
    answer = () => new Promise<SearchResult>((r) => (release = r));
    mount();
    await type("aud");
    await settle();
    expect(calls.map((c) => c.q)).toEqual(["aud"]);
    expect(screen.getByRole("status").textContent).toContain("Searching");
    await type("audi");
    expect(calls[0].signal.aborted).toBe(true);
    await settle();
    expect(calls.map((c) => c.q)).toEqual(["aud", "audi"]);
    release(HITS);
    await waitFor(() => expect(screen.queryByRole("status")).toBeNull());
  });

  it("sends nothing for one character and for the keystrokes inside one pause", async () => {
    mount();
    await type("a");
    await settle();
    expect(calls).toEqual([]);
    await type("au");
    await type("aud");
    await settle();
    expect(calls.map((c) => c.q)).toEqual(["aud"]);
  });

  it("holds the Ask row back while a search is out, so Enter cannot take it early", async () => {
    answer = () => new Promise<SearchResult>(() => {});
    mount();
    const f = await type("auditorium");
    await settle();
    expect((await screen.findAllByRole("option").catch(() => [])).map((o) => o.textContent)).not.toContain("Ask omnibot: auditorium");
    fireEvent.keyDown(f, { key: "Enter" });
    expect(screen.getByTestId("where").textContent).toBe("/");
  });
});

describe("palette keyboard and focus", () => {
  it("closes on Escape and returns focus to what opened it", async () => {
    const { onClose, trigger } = mount();
    const f = await field();
    fireEvent.keyDown(f, { key: "Escape" });
    await waitFor(() => expect(onClose).toHaveBeenCalled());
    await waitFor(() => expect(document.activeElement).toBe(trigger));
  });
});

describe("recent items", () => {
  it("remembers what was opened and shows it before typing, kept in this session per person", async () => {
    const first = mount();
    const f = await type("auditorium");
    await settle();
    await waitFor(async () => expect((await rowTexts()).length).toBeGreaterThan(2));
    fireEvent.keyDown(f, { key: "Enter" });
    await waitFor(() => expect(first.onClose).toHaveBeenCalled());
    expect(JSON.parse(sessionStorage.getItem("og-palette-recent:u-1") ?? "[]")[0]).toMatchObject({ path: `/locations/${uuidFor("loc-aud")}` });

    fireEvent.click(first.trigger);
    await waitFor(async () => expect((await rowTexts())[0]).toContain("auditorium"));
    const list = await screen.findByRole("listbox");
    expect(groupHeadings(list)[0]).toBe("Recent");
  });
});
