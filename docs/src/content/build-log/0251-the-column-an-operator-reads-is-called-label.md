---
title: "The column an operator reads is called `label`"
date: 2026-08-14
refs: []
form: bullet
---

- **The column an operator reads is called `label`**
  ([#613](https://github.com/hyperscaleav/omniglass/issues/613), ADR-0118). `display_name` becomes
  `label` on all twenty-three tables that carry it, and `display_name_generated` becomes
  `label_generated` on the three that carry a pen. The column is **nullable**, and "unset" is SQL
  NULL and nothing else, normalized in Go at the gateway write path. One sweep: the migrations, the
  Storage Gateway, the Huma bodies, every generated artifact, the console, and the docs. A breaking
  wire change, and the CLI flag `--display-name` becomes `--label`.

  The rename **finishes a word rather than introducing one**. `internal/label/` is the rule engine,
  `label_rule` is a column on five tables plus a table of its own, and the API already said
  `:renderLabel`, `:previewLabels` and `:recomputeLabels`, so until this slice a route called
  `:recomputeLabels` wrote a column called `display_name` and the console's `labelGenerated` read
  `display_name_generated`.

  **The ordering was the trap, and it had already sprung.** Postgres sorts NULL last under a default
  `ASC` and the empty string FIRST, so an `order by display_name, name` means opposite things
  depending on which spelling of unset the column carries. Seven registry lists were spelled that
  way, and every one of those tables was storing `''`: vendor, driver, product, standard,
  system_type, component_type and location_type were all already floating their unlabelled rows to
  the TOP of the picker. The test came first and was RED on all seven.

  **Then the storage decision reversed, before merge, on the strength of this slice's own diff.**
  The first version of it made the column `NOT NULL DEFAULT ''`, which forced all seven of those
  orderings to be written `order by nullif(label, '') nulls last`. Seven reads undoing the storage
  choice in one slice is the schema telling you which value it should have stored. `label` is
  nullable now, unset is NULL, the orderings are `order by label nulls last, name`, and what keeps
  NULL the only spelling is `labelOrNull` / `labelPatch` in `internal/storage/label_unset.go`, the
  one place the rule lives. The ordering test stayed RED through the reversal and green after it,
  re-run against the new spelling rather than assumed equivalent, and it now guards the WRITE side:
  break the helper or miss a create path and it goes red on that registry, which is how
  `CreateStandard` was caught still binding its label raw.

  A PATCH carries a set flag beside the value, because clearing a label and leaving it alone are
  different instructions that both arrive as SQL NULL. Reads coalesce, so nothing above the gateway
  learns the column is nullable and no generated artifact moves except the schema facts.

  Two things the sweep had to be told about. `registry_shadow.image` is an operator's forked copy of
  a registry row stored as jsonb keyed by column name, so a column rename cannot reach it and the
  backfill rewrites the key; without that, an operator who had relabelled a forked type would have
  lost the relabelling silently. `audit_log.old` / `audit_log.new` are left exactly as written,
  because they record what a row looked like under the field names it had at the time.

  The word also collides with the console's own `label`, the prop a Button, an InfoTip, a nav item
  and a facet chip already took, which made the one-renderer guard match eleven pieces of ordinary
  chrome the moment the column moved. Both of its patterns were re-anchored rather than suppressed:
  the fallback excludes the exact read `props.label` and nothing wider, and the interpolation is
  anchored to an identifier read on the same line, which is the shape of the haystack bug it was
  written for. "Display name" joins the console's retired-word list and the docs denylist.

  `stem` and `abbrev` are not swept: their NULL means inherit-from-ancestor, a third state with real
  content. `label_rule`, `internal/label/`, `label_draft` and the `:renderLabel` family already said
  label and already meant it. `file.name` stays the file's own label
  ([#755](https://github.com/hyperscaleav/omniglass/issues/755)).
