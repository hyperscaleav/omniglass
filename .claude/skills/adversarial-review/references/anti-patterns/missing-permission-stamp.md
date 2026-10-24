## missing-permission-stamp (live)

- **Cue:** a new route registered without the permission gate; a permission string that
  exists on no seeded role; a route whose stamp disagrees with the nav gate that links it.
- **Failure:** an ungated route is reachable by any session (the `POST /nodes:claim`
  allow-list is the one deliberate exception); a dead stamp gates nothing and teaches a
  phantom permission in the pedagogical Roles view.
- **Fix:** every route carries its `<resource>:<action>` gate at registration; the
  permission resolves against the seeded role matrix.
