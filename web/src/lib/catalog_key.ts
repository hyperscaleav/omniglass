// A catalog row's address (ADR-0318, #1434): every catalog route reads a key
// in one namespace, /<collection>/{namespace}/{key}, so the console carries
// the row's namespace beside its name or id wherever it addresses one.
export type CatalogNameRef = { namespace: string; name: string };
export type CatalogIdRef = { namespace: string; id: string };

// catalogAddress renders a row's key as an operator reads and types it:
// namespace/key, og for the platform's shipped rows, local for the operator's.
export const catalogAddress = (row: CatalogNameRef): string => `${row.namespace}/${row.name}`;
