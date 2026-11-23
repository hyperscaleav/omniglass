import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Router, Route } from "@solidjs/router";
import type { Component } from "solid-js";
import { ME_KEY, type Me } from "../lib/auth";
import ActionRules from "./ActionRules";
import CalcRules from "./CalcRules";
import CommandTypes from "./CommandTypes";
import ComponentTypes from "./ComponentTypes";
import Drivers from "./Drivers";
import EventTypes from "./EventTypes";
import LocationTypes from "./LocationTypes";
import Metrics from "./Metrics";
import NotificationTargets from "./NotificationTargets";
import Notifications from "./Notifications";
import Products from "./Products";
import Properties from "./Properties";
import SeverityLevels from "./SeverityLevels";
import Standards from "./Standards";
import SystemTypes from "./SystemTypes";
import Tags from "./Tags";
import Vendors from "./Vendors";

// The guard for #2431 item 4: every catalog page's create form opens on the same
// two fields, Label then Name, with kind-specific fields after them. A new catalog
// page that opens on anything else (Name first, a data type wedged between) fails
// here, so the order cannot drift back. The event rule form is the one exemption:
// it is a different shape (a rule authoring surface) with its own issue.
const PAGES: [string, Component][] = [
  ["action rules", ActionRules],
  ["calc rules", CalcRules],
  ["command types", CommandTypes],
  ["component types", ComponentTypes],
  ["drivers", Drivers],
  ["event types", EventTypes],
  ["location types", LocationTypes],
  ["metrics", Metrics],
  ["notification targets", NotificationTargets],
  ["notifications", Notifications],
  ["products", Products],
  ["properties", Properties],
  ["severity levels", SeverityLevels],
  ["standards", Standards],
  ["system types", SystemTypes],
  ["tags", Tags],
  ["vendors", Vendors],
];

const admin: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [] };

describe("catalog create forms open on Label, then Name", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });
  it.each(PAGES)("%s", async (_name, Page) => {
    vi.stubGlobal("fetch", vi.fn(async () => new Response("[]", { status: 200, headers: { "content-type": "application/json" } })));
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], admin);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={Page} /></Router>
      </QueryClientProvider>
    ));
    fireEvent.click(await screen.findByRole("button", { name: /^new /i }));
    const dialog = await screen.findByRole("dialog");
    const labels = Array.from(dialog.querySelectorAll("label")).map((l) => l.textContent?.trim() ?? "");
    expect(labels.slice(0, 2)).toEqual(["Label", "Name"]);
  });
});
