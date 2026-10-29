## owner-pinned-row-blocks-admin-delete (fixed; found by the #1353 review)

- **Cue:** a row a user owns (a personal rule, a saved view) that references an admin-owned registry
  row through a RESTRICT foreign key, while no admin route can read or edit the owned row.
- **Failure:** the admin's delete of the registry row is refused naming nothing they can reach: a
  viewer's private row pins the catalog, and the only remedy is deleting the user.
- **Fix:** release the owned row in the delete's own transaction (set the reference to NULL and fall
  back to the default, `DeleteNotificationType` in internal/storage/notification_types.go), or keep
  owned rows off admin-owned references altogether.
