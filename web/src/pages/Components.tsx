import { Show } from "solid-js";
import { useParams } from "@solidjs/router";
import CreatePage from "../components/CreatePage";
import ComponentLeaf from "./ComponentLeaf";

// Components: the identity route. /components/<id> is the component's workspace (ADR-0129,
// ADR-0132) and /components/create the one form, empty. There is no component list: the
// bare /components address lands on Explore, whose outline lists every component where it
// sits (#861).
export default function Components() {
  const zoomParams = useParams();
  // The branch is a reactive Show, not a one-time return: create's post-save
  // navigate lands on the new row's uuid WITHOUT remounting this route
  // component (same /:id pattern), so a decision taken once at setup would
  // leave the create face mounted forever with an empty body (the e2e create
  // handoff walk is the regression that caught it).
  return (
    <Show when={!!zoomParams.id && zoomParams.id !== "create"} fallback={<CreatePage kind="component" />}>
      <ComponentLeaf />
    </Show>
  );
}
