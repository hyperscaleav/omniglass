# The template catalog reads from the binary
#
# The catalog ships inside the binary: four kinds of example an operator can
# start from, none of them a row (#1323). List it by kind, read one whole, and
# an unknown name is a 404.

Type "# The catalog, by kind: what this binary ships"
Enter
Wait
Type `omniglass template list | jq -r '.templates | group_by(.kind)[] | "\(.[0].kind): \(map(.name) | join(" "))"'`
Enter
Wait

Type "# One kind at a time: the system templates and the standard each builds to"
Enter
Wait
Type `omniglass template list --kind system_template | jq -c '.templates[] | {name, standard: .system.standard, roles: (.system.roles | keys)}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# A variant reads whole: its parent, and the role its size pins"
Enter
Wait
Type `omniglass template get mr55 | jq -c '{name, parent: .standard.parent, pinned: [.standard.roles[] | select(.pinned_products) | {name, pinned_products}], metrics: [.standard.metrics[].name]}'`
Enter
Wait
Type "# A component template presets every driver input but the address"
Enter
Wait
Type `omniglass template get kestrel-vroom | jq -c '{name, product: .component.product, inputs: .component.inputs}'`
Enter
Wait
Type "# An unknown name is not in the catalog"
Enter
Wait
Type `omniglass template get boardroom 2>/dev/null | jq -r .detail || true`
Enter
Wait
