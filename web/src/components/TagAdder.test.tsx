import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TagAdder from "./TagAdder";
import { TAGS_KEY, entityTagsKey, type Tag, type TagBinding, type EntityKind } from "../lib/tags";

const registry: Tag[] = [
  { id: "environment", name: "environment", applies_to: [], propagates: true },
  { id: "category", name: "category", applies_to: ["component"], propagates: true },
  { id: "rack_position", name: "rack_position", applies_to: ["location"], propagates: true },
];

function mount(opts: { kind?: EntityKind; canUpdate?: boolean; canCreateKey?: boolean; bindings?: TagBinding[] } = {}) {
  const kind = opts.kind ?? "component";
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...TAGS_KEY], registry);
  qc.setQueryData([...entityTagsKey(kind, "codec")], opts.bindings ?? [{ key: "environment", value: "prod" }]);
  return render(() => (
    <QueryClientProvider client={qc}>
      <TagAdder kind={kind} name="codec" canUpdate={opts.canUpdate ?? true} canCreateKey={opts.canCreateKey ?? false} />
    </QueryClientProvider>
  ));
}

describe("TagAdder", () => {
  it("renders each direct binding as a key=value chip", () => {
    const { container } = mount({ bindings: [{ key: "environment", value: "prod" }, { key: "category", value: "codec" }] });
    const chips = container.querySelectorAll(".tag-pill");
    const text = Array.from(chips).map((c) => c.textContent);
    expect(text.some((t) => t?.includes("environment") && t?.includes("prod"))).toBe(true);
    expect(text.some((t) => t?.includes("category") && t?.includes("codec"))).toBe(true);
  });

  it("with the update permission, offers the add input and a remove control per chip", () => {
    const { getByPlaceholderText, getByLabelText } = mount({ canUpdate: true });
    expect(getByPlaceholderText(/type a key/i)).toBeTruthy();
    expect(getByLabelText("Remove environment")).toBeTruthy();
  });

  it("without the update permission, shows chips read-only (no add input, no remove)", () => {
    const { container, queryByPlaceholderText } = mount({ canUpdate: false });
    expect(queryByPlaceholderText(/type a key/i)).toBeNull();
    expect(container.querySelector('[aria-label="Remove environment"]')).toBeNull();
    expect(container.querySelector(".tag-pill")).toBeTruthy(); // chip still shown
  });

  it("shows the empty state when the entity has no tags", () => {
    const { getByText } = mount({ bindings: [] });
    expect(getByText(/no tags on this component/i)).toBeTruthy();
  });
});
