---
title: "A console subject is a stamp on the shared primitives, gathered by one modifier-click handler"
date: 2026-11-18
status: "Accepted; superseded in part by [ADR-2162](#adr-2162-omnibot-is-handed-the-page-not-what-the-person-gathers)"
summary: "What a person hands omnibot from the console is references and the page as a query, never what the console rendered. A row says what it is by a subject stamp (data-subject-kind and data-subject-ref) that the shared list, tree, blade and alarm primitives set, read by one console-wide alt-click handler, rather than by the ViewResult renderer, which is still Design. A run takes the gathered subjects as AG-UI context and, for one-click triage, an embedded skill by name the server renders"
refs: [2122, 523, 2121, 2026, 1167]
---

- **Date:** 2026-11-18 | **Status:** Accepted; superseded in part by [ADR-2162](#adr-2162-omnibot-is-handed-the-page-not-what-the-person-gathers) | **Pages:** [UI](/architecture/ui/#handing-the-console-to-omnibot), [AI](/architecture/ai/), [connect an agent](/guides/agents/#omnibot-in-the-console)
- **Decision:** (#2122, on the server run of [ADR-2121](/architecture/decisions/#adr-2121-the-server-runs-omnibot-as-the-person-streaming-ag-ui).)
  1. **Context is references, not renders.** A subject is a kind and a name, dotted address or rule
     (`alarm` is `component/rule`, else `component@raised_at`), or the page as its route and
     arguments. Never a uuid and never displayed text, so device-written strings and anything revealed
     on screen stay out of the model's context; omnibot reads the data itself, through its tools,
     under the person's grants. The run frames each entry as quoted data ahead of the person's message.
  2. **The stamp lives on the shared primitives.** `FlatList`, `Outline`, the entity blades and the
     alarm rows set `data-subject-kind` and `data-subject-ref` from their row, so a page built from
     them is gatherable with no code of its own. One capture-phase handler reads the stamp: alt-click
     adds a row, shift-alt-click every stamped sibling from the last one gathered, and a control
     inside a row keeps its own meaning. ⌘ and Ctrl are left to the browser's open-in-new-tab.
  3. **⌘J hands over the page** when nothing else is gathered, and opens omnibot. A detail page
     passes its entity's name or address to the `Page` primitive, which ⌘J puts in place of the
     uuid its route holds.
  4. **Triage is an embedded skill, by name.** A run's `forwardedProps.skill` names a skill and its
     arguments; the server renders the skill's own prompt ([ADR-2026](/architecture/decisions/#adr-2026-the-mcp-server-serves-embedded-skills-as-prompts)),
     so the console never composes skill text. An unknown skill or a missing argument is a 422.
- **Divergence:** the [UI page](/architecture/ui/#the-renderer-contract-viewresult-and-the-views-bff)
  puts one renderer per view behind `ViewResult` (#523), where a column could declare what a row is.
  That renderer is not built, so the stamp rides the list primitives the console does have; when
  `ViewResult` lands, its rows take the same stamp and the handler is unchanged.
- **Not chosen:**
  - **Sending what the console rendered.** A screen holds device-written text and, on some pages,
    revealed values; a reference keeps both out and lets omnibot read under its own scope.
  - **A per-page "add to omnibot" button.** Every page would code its own; the stamp makes it a
    property of the primitives.
  - **Ctrl or ⌘ as the gesture.** Both open a link in a new tab, which a row's link must keep.
  - **Composing the triage prompt in the console.** It would fork the skill's text from the one
    the MCP server serves.
