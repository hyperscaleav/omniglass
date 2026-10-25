## missing-permission-stamp (live)

- **Cue:** a new route registered without the permission gate; a permission string that
  exists on no seeded role; a route whose stamp disagrees with the nav gate that links it.
- **Failure:** an ungated route is reachable by any session (`POST /nodes:claim` and the
  webhook receiver `/webhooks/{token}`, ADR-1024, are the deliberate public exceptions on the
  allow-list); a dead stamp gates nothing and teaches a
  phantom permission in the pedagogical Roles view.
- **Fix:** every route carries its `<resource>:<action>` gate at registration; the
  permission resolves against the seeded role matrix.
