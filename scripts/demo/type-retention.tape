# A type keeps its own retention window
#
# The server keeps each sample lane for a window of days: metric samples 14,
# property transitions 90. A metric or property type can keep a window of its
# own, overriding its lane's, set by a method of its own that works on a shipped
# type too: the type's definition is the seed's, how long an install keeps its
# samples is the install's, and an upgrade never resets it. Null returns the
# type to its lane's window (#417).

Type "# The lanes' windows, from the retention settings"
Enter
Wait
Type `omniglass setting list | jq -c '.values.retention | {metric_days, property_days}'`
Enter
Wait

Type "# A busy, shipped probe metric: keep it three days"
Enter
Wait
Type `omniglass metric-type setRetention tcp-connect-time --days 3 | jq -c '{name, official, retention_days}'`
Enter
Wait

Type "# A shipped property worth a year"
Enter
Wait
Type `omniglass property-type setRetention endpoint-reachable --days 365 | jq -c '{name, official, retention_days}'`
Enter
Wait

Type "# Null returns a type to its lane's window"
Enter
Wait
Type `omniglass metric-type setRetention tcp-connect-time --days null | jq -c '{name, retention_days}'`
Enter
Wait
