# A tape that needs no database
#
# Declares nothing, so the recorder hands it the lab's server and token and no
# DSN: the CLI cannot reach a database the tape never asked for.

Hide
Type `demo_hush 'echo "dsn=[${OMNIGLASS_DSN-unset}]"'`
Enter
Wait
Show
Type "# No database was asked for, so none was handed over"
Enter
Wait
Type 'echo "dsn=[${OMNIGLASS_DSN-unset}]"'
Enter
Wait
