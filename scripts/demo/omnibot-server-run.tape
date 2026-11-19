# The server runs omnibot, streaming AG-UI
#
# A conversation runs on the server: the person's prompt goes to
# assistant conversation run, and the run comes back as AG-UI events. The run
# reads on its own, then ends on an interrupt for the write its agent policy
# asks about; a second run resumes it with the answer. The write is audited as
# omnibot's, and the token the run held is revoked. The lab's server plays a
# recorded model, so the run is the same every time (#2121).

Type "# Start a conversation and ask omnibot to tag a video bar"
Enter
Wait
Type `omniglass assistant conversation create --name lobby | jq -c '{name}'`
Enter
Wait
Type `omniglass assistant conversation run lobby --messages '[{"role":"user","content":"tag the meeting 405 video bar with its asset id"}]' | jq -Rc 'ltrimstr("data: ") | fromjson? | [.type, .toolCallName // .outcome.type // empty, .outcome.interrupts[0].id // empty]'`
Enter
Wait

Type "# The read ran; the tag waits on the interrupt. Approve it with a resume"
Enter
Wait
Type `omniglass assistant conversation run lobby --resume '[{"interruptId":"call-2","status":"resolved","payload":{"approved":true}}]' | jq -Rc 'ltrimstr("data: ") | fromjson? | [.type, .delta // .outcome.type // empty]'`
Enter
Wait

Type "# The write is filed as omnibot's, and the run's token is already revoked"
Enter
Wait
Type `omniglass audit-log list --channel omnibot --limit 1 | jq -c '.events[] | {actor_name, agent, channel, verb, resource}'`
Enter
Wait
Type `omniglass session list | jq '[.sessions[] | select(.description == "omnibot run")] | length'`
Enter
Wait
