---
title: "`setting update` carries a merge patch (#1333)"
date: 2026-11-14
refs: [1333, 618]
---

`omniglass setting update <namespace>` had no way to say what to change: the settings PATCH body is
a free-form object (an RFC 7386 merge patch whose keys belong to the namespace), the CLI generator
only made flags from declared properties, and the command sent a nil body. A platform setting such as
`notification.egress_allow` was changeable only from the console or a raw HTTP call. The generator now
gives any object body with no declared properties a `--body` JSON flag and a `--body-file` (`-` for
stdin), shows the schema's worked example in the command's help, and refuses a malformed or non-object
document before any request. Generator and CLI tests cover the rendered leaf, the exact wire body,
`null` restoring a key, the file and stdin spellings and each refusal. Per-key flags per namespace
are filed as a follow-up.
