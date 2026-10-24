---
title: "The `component_type` registry returns as the device-class genus"
date: 2026-08-07
status: "Accepted; partially reverses [ADR-0047](#adr-0047-the-fields-fold-product_property-and-property_value)"
summary: "The `component_type` registry returns as a nested taxonomy classifying the product, not the component: it carries the identity facts that span products (naming stem, display name, icon, abbrev, default tags), inheriting down the tree with override at any node"
---

- **Date:** 2026-08-07 | **Status:** Accepted | **Pages:** [core entities](/architecture/core-entities/),
  [storage](/architecture/storage/), [Products guide](/guides/admin/products/)
- **Decision:** The **`component_type`** registry returns: a seeded-plus-custom device-class taxonomy
  (`display`, `projector`, `screen`, `presentation-switcher`, `video-bar`, `dsp`, `amplifier`, `mic`,
  `camera`, `codec`, `control-processor`, `touch-panel`, ...) that **nests by `parent_id`** (`mic` over
  `wireless-mic`, `ceiling-mic`, `boundary-mic`), on the same official-and-custom pattern as the other
  classification catalogs, operator-graftable at any node. It classifies the **product**
  (`product.component_type_id`, required), so a component inherits its type through the product it is;
  it is not a second classifier on the component. The row carries exactly the identity facts that
  genuinely span products, **inheriting down the tree with override at any node**: `name`, the naming
  **stem** (a subtype names components by its inherited stem unless it overrides), `display_name`,
  `icon` (the console glyph, replacing the too-coarse derivation from `product.kind`), `abbrev` (the
  two-to-three character hostname stem), and default tags. The seed discipline: a subtype exists only
  where a standard's slot would name it; a fact like panel technology stays on the product. It is
  **not** a shape-definer: contracts, declared properties, and drivers stay on the product. The
  division of labour: the type says what a component **is** (one, via its product); the role says what
  a system **needs** (a typed slot, a separate decision the roles epic owns); the capability axis is
  untouched by this decision.
- **Context:** ADR-0047 retired the component-level `component_type` when the fields folded into the
  product contract, and this partially reverses it, deliberately differently shaped: above the product
  rather than beside the component. What forced the return was naming and rendering. A generated
  component name needs a stem in the device-class vocabulary (`display-1`, the `fp1` hostname
  convention), and the platform had no table that speaks it: the role is positional
  (`display-front` says where it sits), the product is a SKU (`qm55`), `product.kind` is three values
  wide, and capabilities are many-valued. Every identity fact the console kept reaching for (icon,
  abbreviation, base tags, name stem) turned out to live at the same missing level, which is the tell
  that the level is real. The economics confirm it: a thousand components, a few dozen products, a
  couple dozen types; each identity fact is authored once at the level it spans.
- **Tracked under** epic [#614](https://github.com/hyperscaleav/omniglass/issues/614).
