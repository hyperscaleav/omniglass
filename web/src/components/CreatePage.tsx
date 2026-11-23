import { Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate, useSearchParams } from "@solidjs/router";
import Button from "./Button";
import FormActionBar from "./FormActionBar";
import { EntityCreateForm } from "./EntityForm";
import TemplateCreate, { CreateModeChoice, useTemplateChoice } from "./TemplateCreate";
import { FormActionsContext, createFormActions } from "../lib/formactions";
import { CREATE_KIND } from "../lib/templates";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { entityLabel } from "../lib/entities";
import { pushToast } from "../lib/toast";

const KIND_NOUN = { location: "Location", system: "System", component: "Component" } as const;

// CreatePage is /<kind>s/create for the fleet kinds: the one form, empty, on
// its own address (#826), with Explore as the way back since the outline is
// where every location, system and component is listed (#861). ?under= prefills
// placement, which is how a row's + creates where the operator stands, and it
// is also where the way back leads and what it is called (#2465). Saving hands
// off to the new row's Overview with a "created" notice: the operator made it,
// so the next thing they want is to see it, not to be dropped into its editor.
//
// The page offers two ways in (#1329): from scratch, the default, or from a
// template of the kind (a location template for a location), carrying ?under= as
// the template's default location. The template face binds a form slot, so the page
// hosts one and draws its rail inline, as the scratch form draws its own.
export default function CreatePage(props: { kind: "location" | "system" | "component" }) {
  const navigate = useNavigate();
  const [params] = useSearchParams();
  const under = () => (Array.isArray(params.under) ? params.under[0] : params.under) || undefined;
  // Back goes to the place the operator pressed + on, and says so; with no
  // place named it is the outline, which lists everything.
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations, enabled: !!under() }));
  const origin = createMemo(() => {
    const id = under();
    const place = id ? (locations.data ?? []).find((l) => l.id === id || l.name === id) : undefined;
    return place ? { href: `/locations/${encodeURIComponent(place.id)}`, label: entityLabel(place) } : { href: "/explore", label: "Explore" };
  });
  const back = () => navigate(origin().href);
  const choice = useTemplateChoice();
  const actions = createFormActions();
  return (
    <section class="fade-in flex max-w-3xl flex-col gap-4">
      <Button class="flex-none self-start" onClick={back}>{"←"} {origin().label}</Button>
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
              onCreated={(created) => {
                pushToast({ tone: "success", title: `${KIND_NOUN[props.kind]} created.` });
                navigate(`/${props.kind}s/${encodeURIComponent(created.id)}`);
              }}
              onCancel={back}
            />
          }
        >
          <div class="flex flex-col gap-5" data-testid="create-from-template">
            <h2 class="text-lg font-semibold tracking-tight">New {props.kind} from a template</h2>
            <FormActionsContext.Provider value={actions}>
              <TemplateCreate kind={CREATE_KIND[props.kind]} location={under()} onDone={(href) => navigate(href)} onCancel={back} />
              <FormActionBar actions={actions} inline />
            </FormActionsContext.Provider>
          </div>
        </Show>
      </div>
    </section>
  );
}
