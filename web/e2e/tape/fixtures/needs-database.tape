# A tape that needs a database
#
# Declares the need: the recorder makes it a database of its own on the lab's
# Postgres and hands it OMNIGLASS_DSN. The tape migrates that database with the
# CLI, which reads the DSN, then reads back which database it was and how many
# migrations it holds.

# demo-needs: database

Hide
Type `demo_hush 'omniglass migrate'`
Enter
Wait@90s
Type `demo_hush 'psql "$OMNIGLASS_DSN" -Atc "select current_database()"'`
Enter
Wait
Type `demo_hush 'psql "$OMNIGLASS_DSN" -Atc "select count(*) > 0 from schema_migrations"'`
Enter
Wait
Show
Type "# The tape's own database, migrated by the CLI that reads OMNIGLASS_DSN"
Enter
Wait
Type `psql "$OMNIGLASS_DSN" -Atc 'select current_database()'`
Enter
Wait
