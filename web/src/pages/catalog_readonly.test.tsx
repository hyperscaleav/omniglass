import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach, vi } from "vitest";
import { cleanup, fireEvent, render, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Router, Route } from "@solidjs/router";
import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import type { Component } from "solid-js";
import { CATALOG_GROUPS } from "../lib/catalog";
import { ME_KEY, type Me } from "../lib/auth";
import { SECRET_TYPES_KEY } from "../lib/secret_types";
import { TEMPLATES_KEY } from "../lib/templates";
import SecretTypes from "./SecretTypes";
import Templates from "./Templates";

// The guard for #2431 item 2: a kind whose API has no write route draws no
// button that is disabled for it (no dead Edit or Delete, no inert New). "Has a
// write route" is read from the generated route table (api/paths.gen.ts, from
// the OpenAPI document), never from a hand list, so a catalog that gains a write
// route leaves this guard and one that loses it joins.

const PATHS_SRC = readFileSync(resolve(process.cwd(), "src/api/paths.gen.ts"), "utf8");

// The console route of a catalog is not always its API collection: three differ.
const API_COLLECTION: Record<string, string> = {
  "/metrics": "/metric-types",
  "/properties": "/property-types",
  "/notifications": "/notification-types",
};

// Methods the generated table lists per path ("/x" and "/x/{name}" both count;
// custom ":verb" routes do not, they are actions, not the kind's own writes).
function methodsOf(collection: string): Set<string> {
  const out = new Set<string>();
  let cur: string | null = null;
  for (const line of PATHS_SRC.split("\n")) {
    const p = line.match(/^ {2}"(\/[^"]+)": \{/);
    if (p) {
      cur = p[1] === collection || p[1].startsWith(`${collection}/{`) ? p[1] : null;
      if (cur && cur.includes(":")) cur = null;
      continue;
    }
    const m = line.match(/^ {4}(get|post|patch|put|delete): \{/);
    if (m && cur) out.add(m[1]);
  }
  return out;
}
const hasWriteRoute = (collection: string) => ["post", "patch", "put", "delete"].some((m) => methodsOf(collection).has(m));

const consolePaths = [...new Set([...CATALOG_GROUPS.flatMap((g) => g.entries.map((e) => e.path)).filter((p): p is string => !!p), "/secret-types"])];
const readOnly = consolePaths.filter((p) => !hasWriteRoute(API_COLLECTION[p] ?? p));

const admin: Me = { user: { id: "u-root", kind: "person" }, person: { username: "root" }, permissions: [">"], fleet_permissions: [">"], grants: [] };

// One fixture per read-only kind: the page and the seeded cache that fills it.
const FIXTURES: Record<string, { Page: Component; key: readonly string[]; rows: unknown[] }> = {
  "/secret-types": {
    Page: SecretTypes,
    key: SECRET_TYPES_KEY,
    rows: [{ id: "00000000-0000-4000-8000-000000000001", name: "api-key", label: "API Key", official: true, fields: [{ name: "key", type: "string", secret: true, origin: "operator" }] }],
  },
  "/templates": {
    Page: Templates,
    key: TEMPLATES_KEY,
    rows: [{ kind: "component_template", name: "kestrel", label: "Kestrel", component: { product: "kestrel", inputs: {} } }],
  },
};

describe("the route table says which catalogs are read-only", () => {
  it("derives secret types and templates as read-only and the rest as writable", () => {
    expect(readOnly).toContain("/secret-types");
    expect(readOnly).toContain("/templates");
    expect(consolePaths.filter((p) => !readOnly.includes(p)).length).toBeGreaterThan(10);
  });
});

describe("no read-only kind renders a disabled button", () => {
  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });
  it.each(readOnly)("%s", async (path) => {
    const fx = FIXTURES[path];
    expect(fx, `read-only catalog ${path} has no fixture here: add one`).toBeDefined();
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    qc.setQueryData([...ME_KEY], admin);
    qc.setQueryData([...fx.key], fx.rows);
    render(() => (
      <QueryClientProvider client={qc}>
        <Router><Route path="*" component={fx.Page} /></Router>
      </QueryClientProvider>
    ));
    expect(document.querySelectorAll("button:disabled, [aria-disabled='true']")).toHaveLength(0);
    fireEvent.click((await screen.findAllByRole("row"))[1]);
    await waitFor(() => expect(document.querySelector("aside[data-blade]")).not.toBeNull());
    expect(document.querySelectorAll("button:disabled, [aria-disabled='true']")).toHaveLength(0);
  });
});
