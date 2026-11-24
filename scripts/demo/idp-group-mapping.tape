# An identity provider group maps to an Omniglass group, and never to an owner
#
# The HQ AV support team already exists at the identity provider as the av-support-hq group. An
# admin maps that groups claim value to an Omniglass group holding an operator grant over hq, so
# each SSO sign-in puts the team's people in it and takes them out when the provider does. A group
# that carries an owner grant cannot be mapped (#2363, ADR-2361).

Hide
Type `demo_hush 'omniglass user-group create --name it-owners && omniglass user-group grant create it-owners --role owner --scope-kind all'`
Enter
Wait
Type "clear"
Enter
Wait
Show

Type "# A group for the HQ AV support team, with an operator grant over hq"
Enter
Wait
Type `omniglass user-group create --name hq-av-support --label 'HQ AV support' | jq -c '{name, label}'`
Enter
Wait
Type `omniglass user-group grant create hq-av-support --role operator --scope-kind location --scope-id hq | jq -c '{role, scope_kind}'`
Enter
Wait
Type "# Map the provider's av-support-hq group to it, keyed by the provider's issuer"
Enter
Wait
Type `omniglass user-group idp-mapping create hq-av-support --claim-value av-support-hq --issuer https://login.microsoftonline.com/contoso/v2.0 | jq -c '{group, claim_value, issuer}'`
Enter
Wait
Type `omniglass user-group idp-mapping list hq-av-support | jq -c '.mappings[] | {claim_value, issuer}'`
Enter
Wait
Type "# No provider group reaches an owner: mapping a group with an owner grant is refused"
Enter
Wait
Type `omniglass user-group idp-mapping create it-owners --claim-value it-admins --issuer https://login.microsoftonline.com/contoso/v2.0 2>/dev/null | jq -r .detail || true`
Enter
Wait
