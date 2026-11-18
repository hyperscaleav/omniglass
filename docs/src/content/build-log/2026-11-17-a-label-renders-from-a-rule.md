---
title: "A label renders from a rule, and a standard is a contract (#2065)"
date: 2026-11-17
refs: [2065, 2033]
---

A template is the catalog entry a component is created from (ADR-2033), so the string a label or a
notification renders from is a rule, and every surface now says so with no alias left behind. One
migration renames `label_rule.default_template` to `default_rule` and `template` to `rule`;
`internal/storage/label_rule_rename_migration_test.go` plants a system and a location rule below it,
migrates forward, and rolls back. The Go, the seed's `rule:` key, the API descriptions of every
label rule and of a notification type's subject and text (written in the rule language), the
generated client and CLI, the console and the docs follow. A standard is the contract a system is
built to, never a blueprint: the Standards nav hint, the system form and the docs say contract. The
vocabulary denylist retires `label template`, `default_template` and `blueprint`, while Go
`text/template`, the catalog template and its start-from senses stay allowed.
