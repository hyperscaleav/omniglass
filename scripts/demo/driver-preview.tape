# A driver spec can be validated, previewed and run now
#
# A spec for a simulated display's web admin, one emit misnamed: validate names
# the fault. Fixed, it previews through the node, the display answering and
# nothing stored. Attached, its poll runs now and the samples land. The same
# preview is an MCP tool, annotated as reaching the component (#1163).

Type "# A display behind HTTP digest, and a login for it"
Enter
Wait
Type `display='hq.west.lobby.$comp.display-1'`
Enter
Wait
Hide
Type `demo_hush 'sim=$(omniglass component simulation get "$display"); host=$(echo "$sim" | jq -r .address); port=$(echo "$sim" | jq -r "[.surface[] | select(.protocol == \"http\")][0].port")'`
Enter
Wait
Show
Type `omniglass secret create --name driver-preview-login --owner-kind platform --secret-type basic-auth --fields '{"username":"admin","password":"boreal"}' | jq -c '{name}'`
Enter
Wait
Sleep 1s

Type "# A spec whose first emit names a key no catalog has: validate names the fault, saving nothing"
Enter
Wait
Type `omniglass driver get boreal-edge-web | jq '.spec | .polls[0].emits[0].name = "model-name"' > spec.json`
Enter
Wait
Type `omniglass driver validate --spec "$(cat spec.json)" | jq -c '{valid, findings: [.findings[] | {function, message}]}'`
Enter
Wait
Sleep 2s

Type "# Fixed, it validates; previewed through the node, the display answers and nothing is stored"
Enter
Wait
Type `jq '.polls[0].emits[0].name = "model-number"' spec.json > fixed.json && omniglass driver validate --spec "$(cat fixed.json)" | jq -c '{valid}'`
Enter
Wait
Type `inputs="{\"host\":\"$host\",\"port\":\"$port\",\"login\":\"driver-preview-login\"}"`
Enter
Wait
Type `omniglass driver preview --node edge-hq --spec "$(cat fixed.json)" --inputs "$inputs" | jq -c '{stored, results: [.results[] | {poll, samples: [.samples[] | {name, text}]}]}'`
Enter
Wait
Sleep 2s

Hide
Type "clear"
Enter
Wait
Show
Type "# Attached with the shipped driver, its poll runs now, and the samples land"
Enter
Wait
Type `omniglass endpoint create --component "$display" --driver boreal-edge-web --node edge-hq --inputs "$inputs" | jq -c '{name, driver}'`
Enter
Wait
Type `omniglass endpoint poll run "$display/http" identity | jq -c '{stored, poll, samples: [.samples[] | .name]}'`
Enter
Wait
Hide
Type `demo_until 60 'omniglass component property sample list "$display" model-number --limit 5 | jq -e "[.samples[] | select(.instance == \"http\")] | length > 0"'`
Enter
Wait@90s
Show
Type `omniglass component property sample list "$display" model-number --limit 5 | jq -c '[.samples[] | select(.instance == "http")][0] | {instance, value}'`
Enter
Wait
Sleep 1s

Type "# The same three are MCP tools; a preview reaches the component, so it says so"
Enter
Wait
Type `omniglass mcp tools | jq -c '.[] | select(.name | test("^(validate-driver-spec|preview-driver|run-endpoint-poll)$")) | {name, read_only, destructive, open_world}'`
Enter
Wait
Sleep 3s
