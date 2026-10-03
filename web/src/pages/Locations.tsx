import { Show } from "solid-js";
import { useParams } from "@solidjs/router";
import CreatePage from "../components/CreatePage";
import LocationZoom from "./LocationZoom";

// Locations: the identity route. /locations/<id> is the location's workspace (ADR-0129,
// ADR-0132) and /locations/create the one form, empty. There is no location list: the
// bare /locations address lands on Explore, whose outline lists every location where it
// sits (#861).
export default function Locations() {
  const zoomParams = useParams();
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<CreatePage kind="location" />}>
      <LocationZoom />
    </Show>
  );
}
