---
title: "An MCP client signs in by its client id metadata document (#2074)"
date: 2026-11-16
refs: [2074, 2048, 1179]
---

An MCP client can now use the https URL of its own metadata document as its `client_id` and skip
registration: the server fetches the document through the egress contract, checks it names itself,
sends codes only to the redirect URIs it lists, and keeps it ten minutes
([ADR-2074](/architecture/decisions/#adr-2074-an-mcp-client-may-name-itself-by-a-metadata-document-url-fetched-through-egress)).
The Connect page names the host serving the document. Before, `TestMCPClientSignsInByMetadataDocument`
(the go-sdk's own client in its metadata-document mode) failed at authorize; it passes now, with the
refresh by URL client id, the one cached fetch, and `TestMetadataDocumentRefusals` and
`TestMetadataDocumentPrivateAddressNeverFetched` proving the refusals.
