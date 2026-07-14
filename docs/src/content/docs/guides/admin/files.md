---
title: Files
description: "The Files directory under Values: upload, find, download, and delete the opaque bytes kept with an estate, deduplicated and content-addressed, with a sensitive tier for confidential ones."
screenshots:
  - id: files
    path: /web/files
    alt: "The Files directory: a flat list of uploaded files with type, size, and a sensitive badge, plus a New file action."
---

**Files** (under Values) is where you keep the **opaque bytes** that go with an estate, a firmware
image, a device config dump, a runbook, a screenshot, a packet capture, each as a searchable **file**
handle over a deduplicated, content-addressed store ([files and blobs](/architecture/files/)). It sits
beside Secrets and Variables as operator-provided content, but unlike them it is not a cascaded value:
it is flat and tenant-wide, not owned at a scope.

::screenshot{#files}

It uses the same filter, column, and list controls as the estate directories, so browsing feels the
same. Two things differ:

- The list is **flat**, not a tree, and tenant-wide rather than scoped to a subtree, so there is no
  parent and no summary board.
- You **upload** rather than fill a form. **New file** opens a drawer with a file picker; the name and
  content type default from the file you choose. Each file's detail offers **Download** and **Delete**.

## Dedup and the content hash

A file's bytes are stored once, keyed by their **content hash** (the `sha256` shown on the detail).
Upload the same bytes twice, as two handles, and they share one stored copy; the second upload adds no
storage. Deleting a file frees its bytes when no other handle still references them, so removing files
reclaims space rather than leaking it.

## Sensitive files

A file can be marked **Sensitive** (shown by a badge in the list). A sensitive file, a competitive
quote, or a config with internal detail, is visible only to the **admin tier**: it is hidden from an
ordinary lister and cannot be opened or downloaded without that tier, the same rule a sensitive
[secret](/guides/admin/secrets/) follows. Ordinary files are shared with anyone who can read files.
Marking a file sensitive, and seeing one, both need the admin tier; you cannot create a sensitive file
without it.

## Who can do what

Reading and downloading ordinary files rides the viewer floor (`file:read`). Uploading is `file:create`
and deleting is `file:delete`, the writes an operator holds. The sensitive tier (`file:read:admin` and
friends), which admin and owner carry, is what gates the confidential ones.
