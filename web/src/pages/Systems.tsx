import { Show } from "solid-js";
import { useParams } from "@solidjs/router";
import CreatePage from "../components/CreatePage";
import SystemZoom from "./SystemZoom";

// Systems: the identity route. /systems/<id> is the system's workspace (ADR-0129,
// ADR-0132) and /systems/create the one form, empty. There is no system list: the
// bare /systems address lands on Explore, whose outline lists every system where it
// sits (#861).
export default function Systems() {
  const zoomParams = useParams();
  // The branch is a reactive Show, not a one-time return: create's post-save
  // navigate lands on the new row's uuid WITHOUT remounting this route
  // component (same /:id pattern), so a decision taken once at setup would
  // leave the create face mounted forever with an empty body (the e2e create
  // handoff walk is the regression that caught it).
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<CreatePage kind="system" />}>
      <SystemZoom />
    </Show>
  );
}
