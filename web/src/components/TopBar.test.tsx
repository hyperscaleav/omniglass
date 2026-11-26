import { describe, it, expect, afterEach, beforeEach, vi } from "vitest";
import { render, screen, fireEvent, cleanup, within } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TopBar from "./TopBar";

// The top bar opens omnibot's panel (#1167) only for a person the shell says
// may run it: no handler, no button.

// /auth/me answers as the server does for nobody signed in (401), so a widget that
// reads the current user (the inbox bell) sees null rather than a list body.
beforeEach(() => {
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const url = input instanceof Request ? input.url : String(input);
    if (new URL(url, "http://x").pathname.endsWith("/auth/me")) return new Response(null, { status: 401 });
    return new Response(JSON.stringify({ items: [] }), { headers: { "Content-Type": "application/json" } });
  });
});

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
});

function bar(onOpenOmnibot?: () => void) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={() => <TopBar section="Admin" title="Overview" onOpenPalette={() => {}} onOpenOmnibot={onOpenOmnibot} />} />
      </Router>
    </QueryClientProvider>
  ));
}

describe("the top bar's omnibot button", () => {
  it("opens the panel", () => {
    const opened = vi.fn();
    bar(opened);
    fireEvent.click(screen.getByRole("button", { name: "Ask omnibot" }));
    expect(opened).toHaveBeenCalledOnce();
  });

  it("is not there for someone who may not run omnibot", () => {
    bar();
    expect(screen.queryByRole("button", { name: "Ask omnibot" })).toBeNull();
  });
});

// The path's own links: the phone-only lead system is not a crumb.
const crumbLinks = (el: HTMLElement) => within(el).getAllByRole("link").filter((a) => !a.closest('[data-testid="lead-system"]'));

describe("TopBar two-line header (#2575)", () => {
  const crumbs = (n: number) => Array.from({ length: n }, (_, i) => ({ key: `c${i}`, label: `Place number ${i}`, href: `/locations/c${i}` }));
  const kind = { key: "kind", label: "Components", href: "/components" };
  const system = { key: "sys", label: "System ABC", href: "/systems/sys" };
  const mount = (n: number, withSystem = true) =>
    render(() => (
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <Router>
          <Route path="*" component={() => <TopBar section="" title="Amplifier-1" kind={kind} system={withSystem ? system : undefined} trail={crumbs(n)} onOpenPalette={() => {}} />} />
        </Router>
      </QueryClientProvider>
    ));

  it("line 1 is the kind link, the one h1, then the system as a link in parentheses", () => {
    mount(2);
    const line1 = screen.getByTestId("header-line-1");
    expect(within(line1).getByRole("link", { name: "Components" }).getAttribute("href")).toContain("/components");
    expect(within(line1).getByRole("heading", { level: 1 }).textContent).toBe("Amplifier-1");
    expect(screen.getAllByRole("heading", { level: 1 })).toHaveLength(1);
    expect(line1.textContent).toBe("Components/Amplifier-1(System ABC)");
    expect(within(line1).getByRole("link", { name: "System ABC" })).toBeTruthy();
  });

  it("no system suffix when the entity has no primary system", () => {
    mount(2, false);
    expect(screen.getByTestId("header-line-1").textContent).toBe("Components/Amplifier-1");
  });

  it("line 2 is the location path, each crumb a link, not part of line 1", () => {
    mount(2);
    const trail = screen.getByTestId("breadcrumb");
    expect(screen.getByTestId("header-line-1").contains(trail)).toBe(false);
    expect(crumbLinks(trail).map((a) => a.textContent)).toEqual(["Place number 0", "Place number 1"]);
    expect(screen.queryByRole("button", { name: "Show hidden path" })).toBeNull();
  });

  it("on a phone the system leads line 2 and line 1 drops the kind prefix", () => {
    mount(2);
    const line1 = screen.getByTestId("header-line-1");
    expect(within(line1).getByRole("link", { name: "Components" }).closest("[data-kind]")!.className).toMatch(/hidden.*sm:flex/);
    const lead = within(screen.getByTestId("header-line-2")).getByTestId("lead-system");
    expect(lead.className).toContain("sm:hidden");
    expect(lead.textContent).toContain("System ABC");
  });

  it("the last crumb truncates with an ellipsis instead of clipping", () => {
    mount(2);
    const links = crumbLinks(screen.getByTestId("breadcrumb"));
    expect(links[links.length - 1].className).toMatch(/truncate/);
    expect(links[links.length - 1].className).toMatch(/min-w-0/);
  });

  it("collapses the middle of a long path to an ellipsis menu that lists them as links", async () => {
    mount(6);
    const trail = screen.getByTestId("breadcrumb");
    expect(crumbLinks(trail).map((a) => a.textContent)).toEqual(["Place number 0", "Place number 5"]);
    fireEvent.click(screen.getByRole("button", { name: "Show hidden path" }));
    const menu = await screen.findByTestId("trail-menu");
    expect(within(menu).getAllByRole("link").map((a) => a.textContent)).toEqual(["Place number 1", "Place number 2", "Place number 3", "Place number 4"]);
  });
});

describe("TopBar list page shape", () => {
  it("keeps Group / Title on line 1 and the hint on line 2", () => {
    render(() => (
      <QueryClientProvider client={new QueryClient()}>
        <Router>
          <Route path="*" component={() => <TopBar section="Admin" title="Roles" hint="Who may do what." onOpenPalette={() => {}} />} />
        </Router>
      </QueryClientProvider>
    ));
    expect(screen.getByTestId("header-line-1").textContent).toContain("Admin /");
    expect(screen.getByTestId("header-line-1").textContent).toContain("Roles");
    expect(screen.getByTestId("header-line-2").textContent).toBe("Who may do what.");
  });
});
