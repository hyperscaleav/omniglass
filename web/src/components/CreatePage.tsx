import { Show } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import Button from "./Button";
import FormActionBar from "./FormActionBar";
import { EntityCreateForm } from "./EntityForm";
import TemplateCreate, { CreateModeChoice, useTemplateChoice } from "./TemplateCreate";
import { FormActionsContext, createFormActions } from "../lib/formactions";
import { CREATE_KIND } from "../lib/templates";

// CreatePage is /<kind>s/create for the fleet kinds: the one form, empty, on
// its own address (#826), with Explore as the way back since the outline is
// where every place, system and component is listed (#861). ?under= prefills
// placement, which is how a row's + creates where the operator stands. Saving
// hands off to the new row's workspace, already editing.
//
// The page offers two ways in (#1329): from scratch, the default, or from a
// template of the kind (a place template for a location), carrying ?under= as
// the template's default place. The template face binds a form slot, so the page
// hosts one and draws its rail inline, as the scratch form draws its own.
export default function CreatePage(props: { kind: "location" | "system" | "component" }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const under = () => (Array.isArray(params.under) ? params.under[0] : params.under) || undefined;
  const back = () => navigate("/explore");
  const choice = useTemplateChoice();
  const actions = createFormActions();
  return (
    <section class="fade-in flex max-w-3xl flex-col gap-4">
      <Button class="flex-none self-start" onClick={back}>{"←"} Explore</Button>
      <div class="card border border-base-300 bg-base-200 og-pad flex flex-col gap-5">
        <Show when={choice.offered()}>
          <CreateModeChoice mode={choice.mode()} onChange={choice.setMode} />
        </Show>
        <Show
          when={choice.mode() === "template"}
          fallback={
            <EntityCreateForm
              kind={props.kind}
              under={under()}
              onCreated={(created) => navigate(`/${props.kind}s/${encodeURIComponent(created.id)}?edit=1`)}
              onCancel={back}
            />
          }
        >
          <div class="flex flex-col gap-5" data-testid="create-from-template">
            <h2 class="text-lg font-semibold tracking-tight">New {props.kind} from a template</h2>
            <FormActionsContext.Provider value={actions}>
              <TemplateCreate kind={CREATE_KIND[props.kind]} place={under()} onDone={(href) => navigate(href)} />
              <FormActionBar actions={actions} inline />
            </FormActionsContext.Provider>
          </div>
        </Show>
      </div>
    </section>
  );
}
