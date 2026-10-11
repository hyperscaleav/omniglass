import Page from "../components/Page";
import BladeStack from "../components/BladeStack";
import OutlineWorkspace from "../components/OutlineWorkspace";
import { BladesContext, createBladeController } from "../lib/blades";
import { fleetRegistry } from "../lib/fleetBlades";

// Explore (#861): every place in the fleet as one outline, and what is in it.
// The outline and everything around it (counts, need-attention, New, the
// filter) is OutlineWorkspace, which a place's detail view also renders,
// rooted at that place (#872).
export default function Explore() {
  const blades = createBladeController();
  return (
    <BladesContext.Provider value={blades}>
      <Page title="Explore" subtitle="Every place in the fleet, and what is in it.">
        <OutlineWorkspace />
      </Page>
      <BladeStack controller={blades} registry={fleetRegistry} />
    </BladesContext.Provider>
  );
}
