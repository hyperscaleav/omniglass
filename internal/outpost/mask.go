package outpost

import (
	"bytes"
	"encoding/json"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/redact"
	ogv1 "github.com/hyperscaleav/omniglass/proto/og/v1"
)

// taskMask is what the outpost masks out of everything task collects: each
// secret field the worklist names, and the forms its http credential rides
// the wire in (a basic authorization's base64 of username and password),
// which a component echoing its request back would show.
func taskMask(task collection.TaskSpec) []string {
	mask := append([]string(nil), task.Mask...)
	if cred, err := driver.ResolveHTTPCredential(task.Session, task.Secrets); err == nil && cred != nil {
		mask = append(mask, cred.Password, cred.Token, cred.Key)
		if cred.Scheme == driver.SchemeBasic {
			mask = append(mask, redact.BasicAuth(cred.Username, cred.Password))
		}
	}
	return mask
}

// maskGroup masks secrets out of every string a task group carries: a
// property's value, a label, an identity, an event's message and payload, a
// log line's text, source, facility, attributes and labels. A JSON payload
// is masked value by value, so it stays the JSON it was.
func maskGroup(g *ogv1.TaskGroup, secrets []string) {
	if len(secrets) == 0 {
		return
	}
	str := func(s string) string { return redact.Secrets(s, secrets...) }
	labels := func(l map[string]string) {
		for k, v := range l {
			l[k] = str(v)
		}
	}
	identity := func(id *ogv1.Identity) {
		if id != nil {
			id.Value = str(id.Value)
		}
	}
	for _, m := range g.Metrics {
		labels(m.Labels)
		identity(m.Identity)
	}
	for _, p := range g.Properties {
		p.ValueJson = string(maskJSON([]byte(p.ValueJson), secrets))
		labels(p.Labels)
		identity(p.Identity)
	}
	for _, e := range g.Events {
		e.Message = str(e.Message)
		e.Payload = maskJSON(e.Payload, secrets)
		identity(e.Identity)
	}
	for _, l := range g.TaskLogs {
		l.Message, l.Source, l.Facility = str(l.Message), str(l.Source), str(l.Facility)
		l.Attributes = maskJSON(l.Attributes, secrets)
		l.Labels = maskJSON(l.Labels, secrets)
	}
}

// maskJSON masks secrets out of each string (key or value) of a JSON
// document, numbers kept as written. Text that is not JSON is masked whole.
func maskJSON(b []byte, secrets []string) []byte {
	if len(b) == 0 {
		return b
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil || dec.More() {
		return []byte(redact.Secrets(string(b), secrets...))
	}
	changed := false
	v = maskValue(v, secrets, &changed)
	if !changed {
		return b
	}
	out, err := json.Marshal(v)
	if err != nil {
		return []byte(redact.Secrets(string(b), secrets...))
	}
	return out
}

func maskValue(v any, secrets []string, changed *bool) any {
	switch t := v.(type) {
	case string:
		m := redact.Secrets(t, secrets...)
		if m != t {
			*changed = true
		}
		return m
	case []any:
		for i := range t {
			t[i] = maskValue(t[i], secrets, changed)
		}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[maskValue(k, secrets, changed).(string)] = maskValue(e, secrets, changed)
		}
		return out
	}
	return v
}
