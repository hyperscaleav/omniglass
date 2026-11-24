import { Show, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import Button from "./Button";
import { Plus } from "./icons";
import { can, useMe } from "../lib/auth";
import { addComponentHref } from "../lib/add_component_flow";

// AddComponentLink: the door into the add-a-component flow (#2460) from where
// the operator stands: a system (its members), a place, a product, or the
// Simulation page (a simulated device). Shown only to a caller who may create
// a component and its endpoint.
export default function AddComponentLink(props: {
  system?: string;
  under?: string;
  product?: string;
  simulated?: boolean;
  label?: string;
  intent?: "action" | "quiet";
  size?: "xs" | "sm";
}): JSX.Element {
  const me = useMe();
  const navigate = useNavigate();
  return (
    <Show when={can(me.data, "component", "create") && can(me.data, "endpoint", "create")}>
      <Button intent={props.intent ?? "quiet"} size={props.size} icon={Plus} onClick={() => navigate(addComponentHref(props))}>
        {props.label ?? "Add a component"}
      </Button>
    </Show>
  );
}
