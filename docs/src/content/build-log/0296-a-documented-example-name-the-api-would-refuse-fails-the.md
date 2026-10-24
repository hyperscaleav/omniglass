---
title: "A documented example name the API would refuse fails the build"
date: 2026-08-27
refs: []
form: bullet
---

- **A documented example name the API would refuse fails the build**
  ([#571](https://github.com/hyperscaleav/omniglass/issues/571)). An operator copying a name out
  of the docs could receive a 422 from the documented example, and nothing noticed: two of these
  were caught by eye during #545 and thirty-odd were not, which is the argument for a gate rather
  than a sweep. `internal/docslint` now runs every name literal in a fenced example through the
  platform's own rule, reached by a new `storage.ValidateEntityName` (the rule collapsed to one in
  #586, so `ValidateName`'s table argument no longer carries any signal for a caller holding a
  literal and no row). Scope is the judgement, not the validator: only NAME positions
  (`name:`/`type:`/`key:` in YAML, `"name":` in JSON, `--name` in a command) inside FENCES.
  `label:` and `description:` hold English, and `id:` holds a uuid the name rule refuses by
  design, so none of the three is validated. Prose is out of scope permanently, because a page
  teaching the rule has to write the illegal spellings to teach them: the properties guide's
  "it cannot drift into `serialNumber` in one place and `serial_number` in another" is correct
  and stays. A comment inside a fence is prose too, which the first draft learned by reporting
  `--name flag.` out of the sentence "there is no `--name` flag". A fence is what a reader copies,
  and nobody pastes a sentence into the API. The surviving offenders are fixed with it: the
  template-input example names the shipped `snmp-community` and `basic-auth` instead of
  `snmp_community` and `ssh_credential` (no SSH-specific secret type ships), the cascade walkthrough
  reads `retry-limit` beside its already-kebab `poll-interval` sibling, and the variables page
  reads `tls-cert`.
