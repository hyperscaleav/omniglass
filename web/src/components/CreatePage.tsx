import { useNavigate, useSearchParams } from "@solidjs/router";
import Button from "./Button";
import { EntityCreateForm } from "./EntityForm";

// CreatePage is /<kind>s/create for the fleet kinds: the one form, empty, on
// its own address (#826), with Explore as the way back since the outline is
// where every place, system and component is listed (#861). ?under= prefills
// placement, which is how a row's + creates where the operator stands. Saving
// hands off to the new row's workspace, already editing.
export default function CreatePage(props: { kind: "location" | "system" | "component" }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const under = () => (Array.isArray(params.under) ? params.under[0] : params.under) || undefined;
  const back = () => navigate("/explore");
  return (
    <section class="fade-in flex max-w-3xl flex-col gap-4">
      <Button class="flex-none self-start" onClick={back}>{"←"} Explore</Button>
      <div class="card border border-base-300 bg-base-200 og-pad">
        <EntityCreateForm
          kind={props.kind}
          under={under()}
          onCreated={(created) => navigate(`/${props.kind}s/${encodeURIComponent(created.id)}?edit=1`)}
          onCancel={back}
        />
      </div>
    </section>
  );
}
