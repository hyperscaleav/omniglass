# A server refuses a database a newer release migrated
#
# omniglass migrate brings a fresh database to this build's head. A newer release then migrates it
# further, which the tape stages by stamping a version past the head into schema_migrations. This
# build's server meets that schema and refuses to boot: it names the newest version applied, the
# newest it knows, and which release to run instead, rather than serve a schema it cannot read
# (#1289). The database is the tape's own, made for it on the lab's Postgres and dropped after.

# demo-needs: database

Type "# A fresh database, brought to this build's head"
Enter
Wait
Type "omniglass migrate"
Enter
Wait@90s
Type "# Its newest applied migration"
Enter
Wait
Type `psql "$OMNIGLASS_DSN" -Atc 'select max(version) from schema_migrations'`
Enter
Wait
Type "# A newer release migrates it further: a version this build does not embed"
Enter
Wait
Type `psql "$OMNIGLASS_DSN" -c "insert into schema_migrations (version) values ('20991231235959')"`
Enter
Wait
Type "# This build's server meets that schema, and refuses to boot on it"
Enter
Wait
Type `omniglass server || echo "exit $?"`
Enter
Wait@60s
