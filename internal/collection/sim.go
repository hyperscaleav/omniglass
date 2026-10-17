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

// SimCapabilities is what a simulator can do where it runs: whether it may
// bind ports below 1024, whether it holds NET_ADMIN (for firewall-backed
// truthful ping and impairments), and any shift applied to every port.
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
// interface placed on the node, its product (whose name selects the
// simulator profile), and the host its interfaces target.
type SimDevice struct {
	ComponentID string `json:"component_id"`
	Name        string `json:"name"`
	Product     string `json:"product"`
	Address     string `json:"address"`
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
	Serving []string `json:"serving"`
	Hits    []SimHit `json:"hits"`
}
