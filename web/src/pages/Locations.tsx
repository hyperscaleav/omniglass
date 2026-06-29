import { Show, For, Suspense } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { Plus, MapPin } from "../components/icons";

// Locations: the live list, scoped by the server to the caller's grants. The
// first real entity view in the console (the design drew it as a stub).
export default function Locations() {
  const navigate = useNavigate();
  const me = useMe();
  const query = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));

  return (
    <Page
      title="Locations"
      subtitle="The place tree: campuses, buildings, floors, and rooms. Each row is scoped to what you may see."
      actions={
        <Show when={can(me.data, "location", "create")}>
          <button class="btn btn-primary btn-sm" onClick={() => navigate("/locations/new")}>
            <Plus size={15} /> New location
          </button>
        </Show>
      }
    >
      <div class="card" style={{ padding: "6px 6px 2px" }}>
        <Suspense fallback={<Empty msg="Loading…" />}>
          <Show when={!query.error} fallback={<Empty msg={`Could not load locations: ${String(query.error)}`} />}>
            <Show when={(query.data?.length ?? 0) > 0} fallback={<Empty msg="No locations yet. Create the first one." icon />}>
              <table class="tbl">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Type</th>
                    <th>Display name</th>
                  </tr>
                </thead>
                <tbody>
                  <For each={query.data}>
                    {(loc) => (
                      <tr class="clickable" onClick={() => navigate(`/locations/${encodeURIComponent(loc.name)}`)}>
                        <td class="mono" style={{ "font-weight": 600 }}>{loc.name}</td>
                        <td><span class="badge" style={{ color: "var(--text-soft)", "border-color": "var(--line-strong)", background: "var(--raised-2)", "text-transform": "capitalize" }}>{loc.location_type}</span></td>
                        <td style={{ color: "var(--text-dim)" }}>{loc.display_name || "—"}</td>
                      </tr>
                    )}
                  </For>
                </tbody>
              </table>
            </Show>
          </Show>
        </Suspense>
      </div>
    </Page>
  );
}

function Empty(props: { msg: string; icon?: boolean }) {
  return (
    <div style={{ padding: "40px 16px", "text-align": "center", color: "var(--text-dim)", display: "flex", "flex-direction": "column", "align-items": "center", gap: "10px" }}>
      <Show when={props.icon}><span style={{ color: "var(--text-faint)" }}><MapPin size={24} /></span></Show>
      <span style={{ "font-size": "13px" }}>{props.msg}</span>
    </div>
  );
}
