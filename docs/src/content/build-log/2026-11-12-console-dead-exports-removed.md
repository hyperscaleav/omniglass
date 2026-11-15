---
title: "Console dead exports removed (#851)"
date: 2026-11-12
refs: [851, 834, 1203]
---

Removed dead code found during the explorer readiness pass (#834). The following exports had no callers outside their own tests:

- `getSystem`, `getLocation`, `getComponent` (lib/systems.ts, lib/locations.ts, lib/components.ts)
- `setComponentProperty`, `clearComponentProperty` (lib/component_properties.ts)
- `systemTypeByName`, `resolveSystemTypeIcon` (lib/system_types.ts)
- `useIsMobile` (lib/format.ts)
- Icons: `Package`, `Sun`, `Star` (components/icons.tsx)
- The entire module `lib/product_properties.ts`; the ProductContractEditor test, its only importer, now seeds the cache through `classifierPropertiesKey` directly

Cleaned up `ListCtx` in TreeList to include only the members actually used by Files.tsx and its tests: `full`, `openEdit`, `openCreate`, `parentOf`, `byId`. Removed members: `openNode`, `go`, `openFull`, `pushBlade`, `openBlade`, `popBlade`, `closeBlades`, `setFullPage`, `pathOf`. The full page also drops its ancestor crumbs (Files is flat, so the path was always empty) and its own edit slot with `ListCtx.edit`, which no detail body read.
