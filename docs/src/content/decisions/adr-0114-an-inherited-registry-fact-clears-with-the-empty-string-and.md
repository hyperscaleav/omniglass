---
title: "An inherited registry fact clears with the empty string, and the pattern is what admits it"
date: 2026-08-14
status: "Accepted"
summary: "`stem`, `abbrev` and `icon` on `component_type` and `system_type` join the **three-state string sentinel** their `label_rule` neighbour already honoured: omitted is unchanged, an explicit `\"\"` clears the column to NULL so the inheritance walk resumes, a value sets. Not the mask, which ADR-0106 scopes to nullable OBJECT fields because an object has no empty value to overload. The clearing spelling is a pattern **alternation**, `^([a-z0-9][a-z0-9-]*)?$` with `minLength` dropped from the PATCH body (the same spelling `name_rule.stem` ships), so exactly one new string is admitted and every malformed stem is still a 422; CREATE keeps `minLength: 1`, since a row that does not exist yet has nothing to clear. A **root** cannot clear its stem, the refusal create already gives moved to the second path that reaches the same broken row, stated as one pure function so the two handlers cannot disagree. The fork leg decodes the sentinel too, since a shadow image is read back as the row. Both blades now send `\"\"` for an empty box, so #677's and #656's \"no `''` ever rides the body\" guards **invert** to assert the sentinel rather than being deleted"
---

- **Date:** 2026-08-14 | **Status:** Accepted | **Pages:** [the API](/architecture/api/), [core entities](/architecture/core-entities/)
- **Decision:** `stem`, `abbrev` and `icon` on `component_type` and `system_type` join the
  **three-state string sentinel** their `label_rule` neighbour has always honoured: an omitted field
  is unchanged, an explicit `""` clears the column to NULL so the inheritance walk resumes at the
  nearest ancestor, and a value sets. All four columns are the same `case ... nullif(...)` line in
  both handlers, and the **fork leg decodes the sentinel too**, since a shadow image is read back as
  the row. The mask is **not** the instrument
  ([ADR-0106](#adr-0106-a-location-type-is-platform-owned-and-a-nullable-object-clears-under-the-mask)
  scopes mask-with-no-value to nullable OBJECT fields, on the ground that an object has no empty
  value to overload;
  [ADR-0091](#adr-0091-an-update_mask-says-which-fields-a-patch-writes) keeps the sentinel for
  strings).
- **The clearing spelling is a pattern alternation, not a dropped rule.** `stem` carried
  `minLength: 1` and `^[a-z0-9][a-z0-9-]*$` on the PATCH body, so `""` was a 422 in the validator
  **before the handler ran** and the capability was unreachable from every client. The patch's
  `minLength` goes and the character rule is wrapped in an optional group,
  `^([a-z0-9][a-z0-9-]*)?$`, which is the spelling `location_type.name_rule.stem` already ships. That
  admits exactly one new string. `Bad Stem`, `-leading-hyphen`, `UPPER` and a trailing space are all
  still refused, on both registries, and there is an e2e that says so. **CREATE keeps
  `minLength: 1`**: there is nothing to clear on a row that does not exist yet, so `""` there is a
  typo rather than an intent.
- **A root keeps its stem.** A root has no ancestor to inherit from, so clearing its stem is
  `ErrRootComponentTypeNeedsStem` (and its system twin), the same refusal create gives, moved to the
  second path that can reach the same broken row. The rule is one pure function over the patch and
  the parent id, so the two handlers cannot disagree about it, and it is deliberately narrower than
  "a root clears nothing": a root with no icon of its own is answered by the console's fallback,
  where a root with no stem is a name the platform cannot mint.
- **Why this and not the mask, restated for the columns that prompted it:** the mask would work, and
  it would mean two spellings of "clear a string" in one PATCH body, since `label_rule` beside these
  three already clears on `""`. One vocabulary per kind of field is the property worth having.
- **What it cost.** Both console blades now send `""` for an empty box where
  [#677](https://github.com/hyperscaleav/omniglass/issues/677) and
  [#656](https://github.com/hyperscaleav/omniglass/issues/656) taught them to send nothing, so the
  three guard tests those slices left ("no `''` ever rides the body") **invert** rather than being
  deleted: they now assert the sentinel. That is the correct reading of both slices. They were
  data-destruction fixes against a coalescing patch, and the patch stopped coalescing here.
- **Found by** [#716](https://github.com/hyperscaleav/omniglass/issues/716), filed out of #677's
  review rather than folded into it, so the destructive bug shipped the day it was found and the
  missing capability stayed honest until it could land on two registries, the wire, validation and
  both consoles at once.
