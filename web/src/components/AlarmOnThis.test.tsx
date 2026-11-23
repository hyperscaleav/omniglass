import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmOnThis from "./AlarmOnThis";
import { ME_KEY, type Me } from "../lib/auth";

const admin: Me = { user: { id: "u", kind: "person" }, person: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [{ role: "owner", scope_kind: "all" }] };
const viewer: Me = { user: { id: "v", kind: "person" }, person: { username: "v" }, permissions: ["*:read"], fleet_permissions: ["*:read"], grants: [{ role: "viewer", scope_kind: "all" }] };

function mount(me: Me) {
  const qc = new QueryClient();
  qc.setQueryData([...ME_KEY], me);
  render(() => (
    <QueryClientProvider client={qc}>
      <Router><Route path="*" component={() => <AlarmOnThis reads="temperature-c" componentId="c-1" label="Temperature" />} /></Router>
    </QueryClientProvider>
  ));
}

describe("AlarmOnThis", () => {
  it("links to the new-rule form with the key and the component filled in", () => {
    mount(admin);
    expect(screen.getByRole("link", { name: "Alarm on Temperature" }).getAttribute("href")).toBe("/event-rules?new=1&reads=temperature-c&component=c-1");
  });

  it("is not offered to someone who may not create a rule", () => {
    mount(viewer);
    expect(screen.queryByRole("link")).toBeNull();
  });
});
