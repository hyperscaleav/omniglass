package collection

import "time"

// The simulator's wire. A simulator runs beside a node and authenticates
// with that node's credential, so it rides the node's subject grant: it may
// ask for the devices behind its own node and report hits on them, never
// another node's.
const (
	// SimlistWildcard / SimHitWildcard are the server-side subscriptions.
	SimlistWildcard = subjectPrefix + "simlist.*"
	SimHitWildcard  = subjectPrefix + "simhit.*"
)

// SimlistSubject is where a simulator requests the devices to serve
// (request-reply, answered into the node's inbox like the worklist).
func SimlistSubject(node string) string { return subjectPrefix + "simlist." + node }

// SimHitSubject is where a simulator publishes batches of hits.
func SimHitSubject(node string) string { return subjectPrefix + "simhit." + node }

// SimopSubject is where the server asks the simulator beside a node to run
// an operation an operator fired from the console (request-reply, the
// server requesting). The simulator subscribes with the node's grant and
// answers each request once.
func SimopSubject(node string) string { return subjectPrefix + "simop." + node }

// SimOperation is one operation fired at a simulated device: an HTTP call
// (Method, Path, Body), a command line (Command, on the interface Protocol),
// or an SNMP get (OID).
type SimOperation struct {
	ComponentID string `json:"component_id"`
	Kind        string `json:"kind"`
	Method      string `json:"method,omitempty"`
	Path        string `json:"path,omitempty"`
	Body        string `json:"body,omitempty"`
	Command     string `json:"command,omitempty"`
	Protocol    string `json:"protocol,omitempty"`
	OID         string `json:"oid,omitempty"`
}

// SimOperationResult is the simulated device's answer.
type SimOperationResult struct {
	Status    int    `json:"status,omitempty"`
	Body      string `json:"body"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
	TookMs    int64  `json:"took_ms"`
}

// SimCapabilities is what a simulator can do where it runs: whether it may
// bind ports below 1024, whether it holds NET_ADMIN (for firewall-backed
// truthful ping and impairments), and the lift applied to its ports below 1024.
type SimCapabilities struct {
	LowPorts   bool `json:"low_ports"`
	NetAdmin   bool `json:"net_admin"`
	PortOffset int  `json:"port_offset"`
}

// SimlistRequest opens every pull, so the server always knows the
// simulator's current capabilities.
type SimlistRequest struct {
	Capabilities SimCapabilities `json:"capabilities"`
}

// SimDevice is one component the simulator may serve: a component with an
// endpoint placed on the node, its product (whose name selects the
// simulator profile), and the host its endpoints target.
type SimDevice struct {
	ComponentID string `json:"component_id"`
	Name        string `json:"name"`
	Product     string `json:"product"`
	Address     string `json:"address"`
	// Impairment is what the operator has broken on the device, if anything.
	Impairment *SimImpairment `json:"impairment,omitempty"`
}

// SimImpairment is what is wrong with a simulated device: unplugged (dark
// on every protocol), or a degraded link. internal/sim/impair applies it.
type SimImpairment struct {
	Unplugged bool    `json:"unplugged"`
	LatencyMs int     `json:"latency_ms"`
	JitterMs  int     `json:"jitter_ms"`
	LossPct   float64 `json:"loss_pct"`
	RateKbit  int     `json:"rate_kbit"`
}

// SimImpairmentStatus is the simulator's account of impairments: whether it
// can apply them at all (and why not), the last apply's error, and the
// devices it currently impairs.
type SimImpairmentStatus struct {
	Available bool     `json:"available"`
	Reason    string   `json:"reason,omitempty"`
	Error     string   `json:"error,omitempty"`
	Impaired  []string `json:"impaired,omitempty"`
}

// SimlistReply is the server's answer: every candidate device on the node.
// The simulator serves those it has a profile and a local address for.
type SimlistReply struct {
	Devices []SimDevice `json:"devices"`
}

// SimHit is one request a simulated device answered.
type SimHit struct {
	ComponentID string    `json:"component_id"`
	Protocol    string    `json:"protocol"`
	Method      string    `json:"method,omitempty"`
	Path        string    `json:"path,omitempty"`
	Port        int       `json:"port,omitempty"`
	Status      int       `json:"status,omitempty"`
	At          time.Time `json:"at"`
}

// SimHitBatch carries the hits a simulator saw since its last publish, and
// the devices it is serving now.
type SimHitBatch struct {
	Serving     []string             `json:"serving"`
	Hits        []SimHit             `json:"hits"`
	Impairments *SimImpairmentStatus `json:"impairments,omitempty"`
}
