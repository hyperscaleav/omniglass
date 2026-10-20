import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { Router, Route } from "@solidjs/router";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { type Component } from "solid-js";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";
import ComponentTypes from "./ComponentTypes";
import CommandTypes from "./CommandTypes";
import Drivers from "./Drivers";
import EventTypes from "./EventTypes";
import LocationTypes from "./LocationTypes";
import Metrics from "./Metrics";
import Products from "./Products";
import Properties from "./Properties";
import Rules from "./Rules";
import SecretTypes from "./SecretTypes";
import Secrets from "./Secrets";
import SeverityLevels from "./SeverityLevels";
import Standards from "./Standards";
import SystemTypes from "./SystemTypes";
import Tags from "./Tags";
import Variables from "./Variables";
import Vendors from "./Vendors";

// #953. A blade title read its row as `row={useXRow(id)}`: the JSX prop
// compiles to a getter, BladeTitle read the getter inside a memo, and every
// read called the hook again and minted a fresh query observer. With live
// (non-Infinity) staleness each new observer refetched the list on mount, the
// refetch re-ran the memo, and the list was requested without end while the
// blade was open (about 1,300 /event-rules requests in 6 seconds on Rules).
//
// Each case opens a registry page's blade against a stale cache and counts
// the page's list fetches: a storm runs into the hundreds within the window,
// a healthy page reads the list a handful of times (the page, the title and
// the body each mount an observer, and each stale mount may fetch once).

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };

type Case = {
  page: Component;
  // The list endpoint the blade title's row hook reads, and the JSON key the
  // server wraps its rows in.
  path: string;
  key: string;
  // Any fields the page needs beyond the identity to render its row.
  extra?: Record<string, unknown>;
};

const CASES: Record<string, Case> = {
  Drivers: { page: Drivers, path: "/drivers", key: "drivers" },
  SeverityLevels: { page: SeverityLevels, path: "/severity-levels", key: "severity_levels", extra: { rank: 35, color: "orange", outage: false } },
  SecretTypes: { page: SecretTypes, path: "/secret-types", key: "secret_types", extra: { fields: [] } },
  EventTypes: { page: EventTypes, path: "/event-types", key: "event_types" },
  Tags: { page: Tags, path: "/tags", key: "tags", extra: { applies_to: ["component"], propagates: false, allowed_values: [] } },
  CommandTypes: { page: CommandTypes, path: "/command-types", key: "command_types", extra: { params: [] } },
  Metrics: { page: Metrics, path: "/metric-types", key: "metric_types" },
  SystemTypes: { page: SystemTypes, path: "/system-types", key: "system_types" },
  Properties: { page: Properties, path: "/property-types", key: "properties" },
  ComponentTypes: { page: ComponentTypes, path: "/component-types", key: "component_types" },
  Vendors: { page: Vendors, path: "/vendors", key: "vendors" },
  Products: { page: Products, path: "/products", key: "products" },
  Standards: { page: Standards, path: "/standards", key: "standards" },
  Secrets: { page: Secrets, path: "/secrets", key: "secrets", extra: { secret_type: "storm-type", owner_kind: "platform" } },
  Variables: { page: Variables, path: "/variables", key: "variables", extra: { value: "1", owner_kind: "platform" } },
  LocationTypes: { page: LocationTypes, path: "/location-types", key: "location_types", extra: { allowed_parent_types: [] } },
  Rules: {
    page: Rules, path: "/event-rules", key: "event_rules",
    extra: {
      selector: { component_types: [], components: [] }, sample_key: "endpoint-reachable", sample_lane: "property",
      fire_criteria: { expr: 'value == "down"' }, fire_sustain: "0s", clear_sustain: "0s",
      fire_event_type: "condition-started", enabled: true, version: 1, open_alarms: 0, pending_streaks: 0,
    },
  },
};

const STORM_CAP = 50;

const json = (body: unknown) => new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });

describe("opening a blade reads its list a bounded number of times (#953)", () => {
  afterEach(() => vi.restoreAllMocks());

  for (const [name, c] of Object.entries(CASES)) {
    it(`${name}: no storm of list refetches with a blade open`, async () => {
      const row = { id: uuidFor(`storm-${name}`), name: "storm-row", label: "Storm Row", official: false, ...c.extra };
      // Default staleness (0): every observer that mounts is stale and may fetch.
      const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
      qc.setQueryData([...ME_KEY], admin);
      let lists = 0;
      vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
        const url = new URL((input as Request).url);
        if (url.pathname.endsWith("/auth/me")) return json(admin);
        if (url.pathname.endsWith(c.path)) {
          lists++;
          // A storm answered at once never yields the event loop, so the
          // test would hang rather than fail: past the cap, park the request.
          if (lists > STORM_CAP) return new Promise<Response>(() => {});
          return json({ [c.key]: [row] });
        }
        return json({});
      });
      const Page = c.page;
      render(() => (
        <QueryClientProvider client={qc}>
          <Router><Route path="*" component={Page} /></Router>
        </QueryClientProvider>
      ));
      fireEvent.click((await screen.findAllByText("Storm Row"))[0]);
      await waitFor(() => expect(document.querySelector("aside[data-blade]")).not.toBeNull());
      const opened = lists;
      await new Promise((r) => setTimeout(r, 300));
      expect(lists - opened).toBeLessThanOrEqual(2);
      expect(lists).toBeLessThan(10);
    });
  }
});
