package collection

import (
	"cmp"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// worklistWire is a worklist reply as it travels (#1583, ADR-1577): its tasks
// grouped by endpoint, so an endpoint's fields ride once however many tasks
// it runs, and the values tasks of many endpoints share (a driver's task
// specs, its session block, a credential) ride once for the reply, in
// dictionaries a task names by index. A flat reply repeated all of them on
// every task, and a headquarters node's reply outgrew the bus's payload limit.
type worklistWire struct {
	Endpoints        []endpointWire                 `json:"endpoints"`
	Specs            []json.RawMessage              `json:"specs,omitempty"`
	Sessions         []*driver.SessionSpec          `json:"sessions,omitempty"`
	Secrets          []map[string]map[string]string `json:"secrets,omitempty"`
	ConfigGeneration int64                          `json:"config_generation"`
	ReportSeconds    int                            `json:"report_seconds,omitempty"`
}

// endpointWire is one endpoint's group: its fields, and the tasks that run
// over it.
type endpointWire struct {
	endpointFields
	Tasks []taskWire `json:"tasks"`
}

// endpointFields is what every task of an endpoint carries alike, each field
// as TaskSpec documents it.
type endpointFields struct {
	ID             string                  `json:"id,omitempty"`
	Name           string                  `json:"name"`
	Transport      string                  `json:"transport"`
	Params         json.RawMessage         `json:"params,omitempty"`
	SessionRetry   int64                   `json:"session_retry,omitempty"`
	HostKey        string                  `json:"host_key,omitempty"`
	CertificatePin string                  `json:"certificate_pin,omitempty"`
	Fingerprint    string                  `json:"fingerprint,omitempty"`
	Rejection      string                  `json:"rejection,omitempty"`
	Ladder         *driver.EffectiveLadder `json:"ladder,omitempty"`
	ComponentID    string                  `json:"component_id,omitempty"`
	PathUpstreams  []PathUpstream          `json:"path_upstreams,omitempty"`
}

// taskWire is one task of a group. Spec, Session and Secrets name an entry of
// the reply's dictionary of that name, counted from 1, so 0 (left off the
// wire) is none: a probe carries no session block.
type taskWire struct {
	ID      string `json:"id"`
	Mode    string `json:"mode"`
	Spec    int    `json:"spec,omitempty"`
	Session int    `json:"session,omitempty"`
	Secrets int    `json:"secrets,omitempty"`
}

// endpointFieldsOf is a task's endpoint fields, an empty list or params read
// as none: both leave the wire alike, so they must not split a group.
func endpointFieldsOf(t TaskSpec) endpointFields {
	f := endpointFields{ID: t.EndpointID, Name: t.EndpointName, Transport: t.Transport, Params: t.EndpointParams,
		SessionRetry: t.SessionRetry, HostKey: t.HostKey, CertificatePin: t.CertificatePin, Fingerprint: t.Fingerprint,
		Rejection: t.Rejection, Ladder: t.Ladder, ComponentID: t.ComponentID, PathUpstreams: t.PathUpstreams}
	if len(f.Params) == 0 {
		f.Params = nil
	}
	if len(f.PathUpstreams) == 0 {
		f.PathUpstreams = nil
	}
	return f
}

// dictionary numbers distinct values from 1 by a key the caller derives from
// the value, so equal values built apart (a credential unsealed once per task)
// share an entry.
type dictionary[T any] struct {
	at     map[string]int
	values []T
}

func (d *dictionary[T]) ref(key string, v T) int {
	if i, ok := d.at[key]; ok {
		return i
	}
	if d.at == nil {
		d.at = map[string]int{}
	}
	d.values = append(d.values, v)
	d.at[key] = len(d.values)
	return len(d.values)
}

// refEncoded names v by its encoding, for a value with no bytes of its own.
func refEncoded[T any](d *dictionary[T], v T) (int, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return 0, err
	}
	return d.ref(string(b), v), nil
}

// groupWorklist is the reply's wire form. Tasks of one endpoint share a group
// only when they agree on every endpoint field; tasks that disagree (none the
// server builds) get a group each, so the wire never drops a difference.
func groupWorklist(w WorklistReply) (worklistWire, error) {
	out := worklistWire{Endpoints: []endpointWire{}, ConfigGeneration: w.ConfigGeneration, ReportSeconds: w.ReportSeconds}
	var specs dictionary[json.RawMessage]
	var sessions dictionary[*driver.SessionSpec]
	var secrets dictionary[map[string]map[string]string]
	groupsOf := map[string][]int{} // endpoint id -> its groups' indexes
	for _, t := range w.Tasks {
		f := endpointFieldsOf(t)
		g := -1
		for _, i := range groupsOf[t.EndpointID] {
			if reflect.DeepEqual(out.Endpoints[i].endpointFields, f) {
				g = i
				break
			}
		}
		if g < 0 {
			g = len(out.Endpoints)
			out.Endpoints = append(out.Endpoints, endpointWire{endpointFields: f})
			groupsOf[t.EndpointID] = append(groupsOf[t.EndpointID], g)
		}
		tw := taskWire{ID: t.ID, Mode: t.Mode}
		if len(t.Spec) > 0 {
			tw.Spec = specs.ref(string(t.Spec), t.Spec)
		}
		var err error
		if t.Session != nil {
			if tw.Session, err = refEncoded(&sessions, t.Session); err != nil {
				return worklistWire{}, fmt.Errorf("collection: task %s session: %w", t.ID, err)
			}
		}
		if t.Secrets != nil {
			if tw.Secrets, err = refEncoded(&secrets, t.Secrets); err != nil {
				return worklistWire{}, fmt.Errorf("collection: task %s secrets: %w", t.ID, err)
			}
		}
		out.Endpoints[g].Tasks = append(out.Endpoints[g].Tasks, tw)
	}
	out.Specs, out.Sessions, out.Secrets = specs.values, sessions.values, secrets.values
	return out, nil
}

// entry is a task's dictionary reference resolved: the zero value for 0, the
// entry for 1 to len(values), an error naming what is missing past that.
func entry[T any](values []T, ref int, what, task string) (T, error) {
	var zero T
	switch {
	case ref == 0:
		return zero, nil
	case ref < 0 || ref > len(values):
		return zero, fmt.Errorf("collection: task %s names %s %d of %d", task, what, ref, len(values))
	}
	return values[ref-1], nil
}

// ungroupWorklist is the reply a wire form carries, its tasks in id order
// (the order the server builds them in). Tasks of one endpoint share its
// params, ladder and upstreams, and tasks naming one dictionary entry share
// it: the node reads a task, never writes one.
func ungroupWorklist(in worklistWire) (WorklistReply, error) {
	if in.Endpoints == nil {
		// A reply with no endpoints list is not this wire (a flat reply from a
		// server built before #1583 reads that way). Decoding it as no tasks
		// would tear down every session; failing the pull keeps the last one.
		return WorklistReply{}, fmt.Errorf("collection: worklist reply carries no endpoints list")
	}
	out := WorklistReply{ConfigGeneration: in.ConfigGeneration, ReportSeconds: in.ReportSeconds}
	for _, ep := range in.Endpoints {
		for _, tw := range ep.Tasks {
			t := TaskSpec{ID: tw.ID, Mode: tw.Mode, EndpointID: ep.ID, EndpointName: ep.Name, Transport: ep.Transport,
				EndpointParams: ep.Params, SessionRetry: ep.SessionRetry, HostKey: ep.HostKey, CertificatePin: ep.CertificatePin,
				Fingerprint: ep.Fingerprint, Rejection: ep.Rejection, Ladder: ep.Ladder, ComponentID: ep.ComponentID,
				PathUpstreams: ep.PathUpstreams}
			var err error
			if t.Spec, err = entry(in.Specs, tw.Spec, "spec", tw.ID); err != nil {
				return WorklistReply{}, err
			}
			if t.Session, err = entry(in.Sessions, tw.Session, "session", tw.ID); err != nil {
				return WorklistReply{}, err
			}
			if t.Secrets, err = entry(in.Secrets, tw.Secrets, "secrets", tw.ID); err != nil {
				return WorklistReply{}, err
			}
			out.Tasks = append(out.Tasks, t)
		}
	}
	slices.SortStableFunc(out.Tasks, func(a, b TaskSpec) int { return cmp.Compare(a.ID, b.ID) })
	return out, nil
}

// MarshalJSON encodes the reply as its grouped wire form.
func (w WorklistReply) MarshalJSON() ([]byte, error) {
	g, err := groupWorklist(w)
	if err != nil {
		return nil, err
	}
	return json.Marshal(g)
}

// UnmarshalJSON decodes the grouped wire form.
func (w *WorklistReply) UnmarshalJSON(b []byte) error {
	var g worklistWire
	if err := json.Unmarshal(b, &g); err != nil {
		return err
	}
	out, err := ungroupWorklist(g)
	if err != nil {
		return err
	}
	*w = out
	return nil
}
