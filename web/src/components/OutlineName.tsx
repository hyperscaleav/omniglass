// One name renderer for every outline row (#2438): a location, a system header,
// a system in the matches and a component all set their name here, in the one
// face. The data font is for numbers and identifiers; a name is a name, so a
// row's kind changes its weight and tone and never its font.
export type NameKind = "location" | "system" | "group" | "component" | "address";

export const NAME_CLASS: Record<NameKind, string> = {
  location: "font-semibold",
  system: "font-semibold",
  group: "text-[13px] font-medium text-base-content/60",
  component: "",
  // A location read as the address of the system it wears (#2757): its name
  // leads the row and the address follows, muted.
  address: "text-base-content/60",
};

export default function OutlineName(props: { kind: NameKind; label: string; class?: string }) {
  return (
    <span data-label class={`truncate ${NAME_CLASS[props.kind]} ${props.class ?? ""}`}>
      {props.label}
    </span>
  );
}
