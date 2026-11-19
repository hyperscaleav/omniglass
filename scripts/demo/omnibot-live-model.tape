# Omnibot runs on the server's model
#
# omniglass omnibot with no --replay runs its conversation on the server, on
# the model the assistant settings name, and confirms each write it asks about
# on the terminal. The lab names no provider, so its server plays its
# recording. Pointing the settings at a provider takes the key as a platform
# secret, by name; nothing shows the key itself. A key that vanishes refuses
# the next run, naming the secret, with no restart in between (#2109).

Type "# The lab names no model provider, so its server plays a recording"
Enter
Wait
Type `omniglass setting list | jq -c '.values.assistant | {provider, model, base_url}'`
Enter
Wait

Type "# Ask omnibot on the server; the read runs on its own, the tag asks first"
Enter
Wait
Type `omniglass omnibot "tag the meeting 405 video bar with its asset id"`
Enter
Wait+Screen /Run it\?/
Type "y"
Enter
Wait

Type "# Seal a model key as a platform secret and point omnibot at it, by name"
Enter
Wait
Type `omniglass secret create --name models-key --owner-kind platform --secret-type api-key --fields '{"key":"sk-or-demo"}' | jq -c '{name, fields: [.fields[] | {name, value}]}'`
Enter
Wait
Type `omniglass setting update assistant --body '{"provider":"openai","key_secret":"models-key"}' | jq -c '.values.assistant | {provider, model, key_secret}'`
Enter
Wait

Type "# A key that vanishes refuses the next run, naming it; no restart, no fallback"
Enter
Wait
Type `omniglass secret delete models-key`
Enter
Wait
Type `omniglass omnibot "hello?" || true`
Enter
Wait

Type "# Back to the recording"
Enter
Wait
Type `omniglass setting delete assistant && omniglass setting list | jq -c '.values.assistant.provider'`
Enter
Wait
