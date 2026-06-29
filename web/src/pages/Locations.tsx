import { Show, For, Suspense } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { Plus, MapPin } from "../components/icons";

// Locations: the live list, scoped by the server to the caller's grants. The
// first real entity view in the console.
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
      <div class="card border border-base-300 bg-base-200">
        <Suspense fallback={<Empty msg="Loading…" />}>
          <Show when={!query.error} fallback={<Empty msg={`Could not load locations: ${String(query.error)}`} />}>
            <Show when={(query.data?.length ?? 0) > 0} fallback={<Empty msg="No locations yet. Create the first one." icon />}>
              <table class="og-rows table table-sm">
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
                      <tr class="cursor-pointer hover:bg-base-content/5" onClick={() => navigate(`/locations/${encodeURIComponent(loc.name)}`)}>
                        <td class="font-data font-semibold">{loc.name}</td>
                        <td><span class="badge badge-soft badge-neutral badge-sm capitalize">{loc.location_type}</span></td>
                        <td class="text-base-content/50">{loc.display_name || "—"}</td>
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
    <div class="flex flex-col items-center gap-2.5 px-4 py-10 text-center text-base-content/50">
      <Show when={props.icon}><span class="text-base-content/30"><MapPin size={24} /></span></Show>
      <span class="text-sm">{props.msg}</span>
    </div>
  );
}
