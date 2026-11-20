---
title: Retire leftover template senses in calc, notifications and nav
date: 2026-11-19
refs: [2106]
---

Four places still used "template" loosely after ADR-2033 established the
canonical split: "template" means only a clonable catalog entry; the Go
`text/template` string a notification renders from is a "rule"; the calc
editor's list-function shortcuts are "starters".

The renames: `CalcTemplate`/`calcTemplates`/`templates` prop to
`CalcStarter`/`calcStarters`/`starters` in the calc editor; Go identifiers
`ParseNotificationTemplate`, `TemplateRefusal` and `notificationTemplateDoc`
to `ParseNotificationRule`, `RuleRefusal` and `notificationRuleDoc`;
`install.md`'s "alarm template" phrase to "event rule"; and
`architecture/expressions.mdx`'s "start-from templates" to "starters".

The nav change removes the two `hint` fields from the `Values` and `Admin`
group items (pathless, so their hints could never reach `navByPath` or
`SectionStub`), makes `NavItem.hint` optional, and adds a nav test
asserting the contract: pathless items carry no hint; every item with a
path carries one.
