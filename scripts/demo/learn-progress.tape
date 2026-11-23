# A person keeps their own lesson progress
#
# The console's Learn page remembers which lessons a person has finished in
# their own settings: the learn namespace, bound at the user level of the
# settings cascade, over the platform's value and nobody else's (#2450). Any
# signed-in person patches their own with setting me update; the platform's
# read is unchanged by it, and null restores the platform's value.

Type "# Mark two lessons done, in your own settings"
Enter
Wait
Type `omniglass setting me update learn --body '{"done":["device-to-samples","rules-and-alarms"]}' | jq -c '.values.learn'`
Enter
Wait

Type "# Read it back: the console's Learn page reads the same"
Enter
Wait
Type `omniglass setting me list | jq -c '.values.learn'`
Enter
Wait

Type "# The platform level is untouched: progress is per person"
Enter
Wait
Type `omniglass setting list | jq -c '{learn: .values.learn, source: .sources["learn.done"]}'`
Enter
Wait

Type "# Null restores the platform's value"
Enter
Wait
Type `omniglass setting me update learn --body '{"done":null}' | jq -c '.values.learn'`
Enter
Wait
