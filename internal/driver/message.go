package driver

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// A message is the unit a held connection carries (#1038, slice 3 of
// #1113): a line of a line protocol, or one server-sent event off an http
// stream, its data with the event's name and id beside it (later, a
// websocket frame). The router offers each to the listeners as it offered a
// line (ADR-0151), and a claimed one is read through the one extraction
// primitive in the format its data is declared in.

// Message is one unit off a held connection.
type Message struct {
	// Data is the line, or an event's data (its data lines joined by a
	// newline).
	Data string
	// Event is a server-sent event's name ("message" when the stream names
	// none); a line has none.
	Event string
	// ID is a server-sent event's own id, empty when it carried none.
	ID string
	// Format is the response format the data is read in (json or text, the
	// stream's declared format; a webhook's json, xml, form or text); empty
	// for a line, which is text.
	Format string
	// Header is a pushed message's headers (a webhook's post, #1024), by
	// canonical name, which a listener's header match reads.
	Header map[string][]string
}

// LineMessage is a line as a message.
func LineMessage(line string) Message { return Message{Data: line} }

// Payload is the message in the forms an emit reads: a line as text, an
// event's data in its declared format, a JSON event read by jsonpath as an
// http answer is.
func (m Message) Payload() Payload {
	if m.Format == FormatForm {
		return formPayload(m.Data)
	}
	if m.Format == "" {
		// A line, or a websocket frame, is text a regex reads, and a JSON
		// one a jsonpath reads too.
		return Payload{Text: m.Data, JSON: []byte(m.Data), docs: &parsedDocs{}}
	}
	p := HTTPPayload([]byte(m.Data), &Request{Format: m.Format})
	p.docs = &parsedDocs{}
	return p
}

// FieldMatch claims a message whose data is a JSON document by one field
// (#1038): the field at a dotted path (the jsonpath grammar) equals a value,
// or matches a regex, compared as the jsonpath extraction renders it.
type FieldMatch struct {
	Path   string `json:"path"`
	Equals string `json:"equals,omitempty"`
	Regex  string `json:"regex,omitempty"`
}

// matches reads the field off a JSON document; data that is not JSON, or
// carries no such field, never matches.
func (f *FieldMatch) matches(data string) bool {
	v, err := jsonPath([]byte(data), f.Path)
	if err != nil {
		return false
	}
	if f.Regex != "" {
		re, err := compiled(f.Regex)
		return err == nil && re.MatchString(v)
	}
	return v == f.Equals
}

// MatchesMessage applies the rule to a message: its event name, when the
// rule names one, then its one data rule (a prefix or a regex on the data,
// or a JSON field); a rule naming only an event claims every event of that
// name. A line has no event name, so a rule naming one never claims a line.
func (m Match) MatchesMessage(msg Message) bool {
	if m.Event != "" && msg.Event != m.Event {
		return false
	}
	if m.Header != nil && !m.Header.matches(msg.Header) {
		return false
	}
	switch {
	case m.Prefix != "":
		return strings.HasPrefix(msg.Data, m.Prefix)
	case m.Regex != "":
		re, err := compiled(m.Regex)
		return err == nil && re.MatchString(msg.Data)
	case m.Field != nil:
		return m.Field.matches(msg.Data)
	}
	return m.Event != "" || m.Header != nil
}

// validateListenerMatch refuses a listener's match no router could apply:
// at most one data rule (a prefix, a regex or a JSON field), an event name
// only on a stream (http), a field match only where the data is JSON, and
// at least one rule.
func validateListenerMatch(what string, m Match, transport string, stream *Request) error {
	data := 0
	for _, set := range []bool{m.Prefix != "", m.Regex != "", m.Field != nil} {
		if set {
			data++
		}
	}
	if data > 1 {
		return fmt.Errorf("driver: %s declares more than one data rule; a match is an event name and at most one data rule (a prefix, a regex or a field)", what)
	}
	if data == 0 && m.Event == "" && m.Header == nil {
		return fmt.Errorf("driver: %s needs a match rule: an event name, a header, a prefix, a regex or a field", what)
	}
	if m.Header != nil {
		if err := validateHeaderMatch(what, m.Header, transport); err != nil {
			return err
		}
	}
	if m.Event != "" && !IsHTTP(transport) {
		return fmt.Errorf("driver: %s names an event, which only a server-sent events stream carries; a line has none", what)
	}
	if m.Regex != "" {
		if _, err := regexp.Compile(m.Regex); err != nil {
			return fmt.Errorf("driver: %s has an uncompilable match regex: %v", what, err)
		}
	}
	if f := m.Field; f != nil {
		if f.Path == "" {
			return fmt.Errorf("driver: %s field match names no path", what)
		}
		if (f.Equals == "") == (f.Regex == "") {
			return fmt.Errorf("driver: %s field match declares one of equals or regex", what)
		}
		if f.Regex != "" {
			if _, err := regexp.Compile(f.Regex); err != nil {
				return fmt.Errorf("driver: %s field match has an uncompilable regex: %v", what, err)
			}
		}
		if stream != nil && stream.FormatOf() != FormatJSON {
			return fmt.Errorf("driver: %s field match reads json, and the data it claims is %s", what, stream.FormatOf())
		}
	}
	return nil
}

// streamFormats are the formats a stream's event data is read in: an
// event's data is a line or a few, a JSON document or text.
var streamFormats = []string{FormatJSON, FormatText}

// streamHeaders are the headers the node sets on a stream request itself:
// the media type it asks for and the id it resumes from.
var streamHeaders = []string{"accept", "last-event-id", "cache-control"}

// validateStream refuses a session stream no node could hold: a GET with a
// path and no body, read as json or text, setting none of the headers the
// node sends itself, and claimed by at least one listener.
func (s *Spec) validateStream() error {
	st := s.Session.Stream
	if st == nil {
		return nil
	}
	if err := validateStreamShape(st); err != nil {
		return err
	}
	if err := validateHTTPRequest("session.stream", *st, false, s.Session); err != nil {
		return err
	}
	if len(s.Listeners) == 0 {
		return fmt.Errorf("driver: session.stream needs a listener to claim its events; with none, nothing holds it")
	}
	return nil
}

// validateStreamShape refuses a stream request's own shape, before the
// listeners read their events in its format.
func validateStreamShape(st *Request) error {
	if m := st.MethodOf(); m != "GET" {
		return fmt.Errorf("driver: session.stream is a GET held open, not %s", m)
	}
	if len(st.Body) > 0 {
		return fmt.Errorf("driver: session.stream sends no body")
	}
	if !slices.Contains(streamFormats, st.FormatOf()) {
		return fmt.Errorf("driver: session.stream's events are read as json or text, not %s", st.FormatOf())
	}
	for h := range st.Headers {
		if slices.Contains(streamHeaders, strings.ToLower(h)) {
			return fmt.Errorf("driver: session.stream may not set the %s header, which the node sends itself", h)
		}
	}
	return nil
}
