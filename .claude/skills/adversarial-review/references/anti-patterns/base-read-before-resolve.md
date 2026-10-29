## base-read-before-resolve (fixed in its one known resolver; found building #896)

- **Cue:** a memoized recursive resolver over a map (profiles extending profiles, templates
  including templates) that reads a dependency's value before the recursive call that replaces it
  in the map.
- **Failure:** the dependent folds over the stale, unresolved dependency whenever map iteration
  reaches it first, so a chain resolves differently from load to load: a profile two levels down
  silently loses its grandparent's faces on about half of all loads, and a one-level test never
  sees it.
- **Fix:** recurse first, then read the dependency from the map; pin it with a three-level chain
  loaded many times (`TestAnExtendsChainResolvesWhole`, internal/sim/profile).
