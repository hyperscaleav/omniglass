import "@testing-library/jest-dom/vitest";
import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route, useLocation } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import SystemEmpty from "./SystemEmpty";
import { ME_KEY, type Me } from "../lib/auth";

// The empty system's state (#2464): it teaches, and it opens the add flow with
// the system filled in.
const admin: Me = { user: { id: "u", kind: "person" }, person: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { user: { id: "v", kind: "person" }, person: { username: "v" }, permissions: ["*:read"], grants: [] };

let landed = "";
function Landing() { const l = useLocation(); landed = `${l.pathname}${l.search}`; return <div />; }

function mount(me: Me, hasStandard = false, onStandard = () => {}) {
  window.history.pushState({}, "", "/");
  landed = "";
  const qc = new QueryClient();
  qc.setQueryData(ME_KEY, me);
  render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="/" component={() => <SystemEmpty systemId="sys-1" hasStandard={hasStandard} onStandard={onStandard} />} />
        <Route path="*" component={Landing} />
      </Router>
    </QueryClientProvider>
  ));
}
afterEach(() => vi.restoreAllMocks());

describe("SystemEmpty", () => {
  it("teaches what a system and a slot are", () => {
    mount(admin);
    expect(screen.getByText("No components yet")).toBeInTheDocument();
    expect(screen.getByTestId("system-empty").textContent).toMatch(/system is the thing Omniglass monitors/);
    expect(screen.getByTestId("system-empty").textContent).toMatch(/slot/);
  });
  it("Add component opens the add flow with the system prefilled", async () => {
    mount(admin);
    fireEvent.click(await screen.findByRole("button", { name: "Add component" }));
    await waitFor(() => expect(landed).toBe("/components/add?system=sys-1"));
  });
  it("Start from a standard calls back, and hides once the system has a standard", async () => {
    const cb = vi.fn();
    mount(admin, false, cb);
    fireEvent.click(await screen.findByRole("button", { name: "Start from a standard" }));
    expect(cb).toHaveBeenCalled();
  });
  it("offers no actions without component:create", () => {
    mount(viewer);
    expect(screen.queryByRole("button", { name: "Add component" })).toBeNull();
    expect(screen.getByText(/component:create/)).toBeInTheDocument();
  });
});
