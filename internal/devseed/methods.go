package devseed

// MethodGap is a collection method no healthy lab component exercises yet,
// and the issue that adds it (#1370). An unbuilt one is not in the binary at
// all (no driver, no registry transport) and is listed so the gap is said
// out loud.
type MethodGap struct {
	Issue   string
	Unbuilt bool
}

// MethodGaps are the collection methods the dev lab does not exercise, keyed
// by driver or transport name. The coverage test fails on a method neither
// exercised nor listed here, and on a listed one the lab exercises now; the
// lab's own check skips them.
var MethodGaps = map[string]MethodGap{
	"websocket":     {Issue: "#1414"},                // a plain-ws face and a driver
	"push-listener": {Issue: "#1148", Unbuilt: true}, // the shared push listener
	"syslog":        {Issue: "#1149", Unbuilt: true},
	"snmp-trap":     {Issue: "#1150", Unbuilt: true},
}
