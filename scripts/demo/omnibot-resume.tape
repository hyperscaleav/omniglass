# Omnibot keeps a conversation for its owner
#
# --resume <name> keeps an omnibot conversation on the server under that name,
# its owner's alone, and a later run with the same name carries it on where the
# last one stopped. A recorded model stands in for a live one. The first run
# reads a video bar and answers; the second picks the conversation up with its
# messages kept and answers a follow-up. The owner lists and reads their
# conversations with the generated assistant conversation commands (#2113).

Hide
Type `demo_hush "printf '%s' '{\"turns\":[{\"tool_calls\":[{\"name\":\"get-component\",\"arguments\":{\"name\":\"hq.west.level-4.meeting-405.\$comp.videobar-1\"}}]},{\"text\":\"The meeting 405 video bar is a conferencing bar, tagged AV-0415.\"}]}' > /tmp/triage-1.json"`
Enter
Wait
Type `demo_hush "printf '%s' '{\"turns\":[{\"text\":\"It is the same video bar we looked at: meeting 405, AV-0415.\"}]}' > /tmp/triage-2.json"`
Enter
Wait
Show

Type "# Ask omnibot about a video bar, keeping the conversation as triage"
Enter
Wait
Type `omniglass omnibot --replay /tmp/triage-1.json --resume triage "what is the meeting 405 video bar"`
Enter
Wait+Screen /AV-0415\./

Type "# Later, a new run picks triage up where it stopped"
Enter
Wait
Type `omniglass omnibot --replay /tmp/triage-2.json --resume triage "which bar was that again"`
Enter
Wait+Screen /we looked at/

Type "# The conversation is the owner's, kept by name"
Enter
Wait
Type `omniglass assistant conversation list | jq -c '.conversations[] | {name}'`
Enter
Wait
Type `omniglass assistant conversation get triage | jq -c '[.messages[].role]'`
Enter
Wait
