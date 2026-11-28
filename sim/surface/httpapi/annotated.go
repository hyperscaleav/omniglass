package httpapi

import "bytes"

// Member is one key of an annotated document: the document a device answered
// from a template that carried a documentation comment ahead of some keys
// (`{{/* @field status.device.cpu_load ... */}}`). A comment renders as nothing
// but leaves its own line, the key's indent and a newline, in the answer, and a
// collector reading the document byte for byte sees it; Noted keeps that line.
type Member struct {
	Key string
	// Value is encoded as JSON, markup left as written; an Object is a nested
	// block.
	Value any
	// Noted says a comment line stands ahead of the key.
	Noted bool
}

// Object is the keys of a JSON object, in the order they are written.
type Object []Member

// Annotated renders an object as the document a device answers: two-space
// indented, one key to a line, a nested Object spread over lines, a Noted key
// preceded by its comment's whitespace-only line, ending in a newline. With no
// Noted key it is Document's layout, in a key order that is the slice's.
func Annotated(o Object) []byte {
	var b bytes.Buffer
	writeObject(&b, o, "")
	b.WriteString("\n")
	return b.Bytes()
}

func writeObject(b *bytes.Buffer, o Object, indent string) {
	in := indent + "  "
	b.WriteString("{\n")
	for i, m := range o {
		if i > 0 {
			b.WriteString(",\n")
		}
		if m.Noted {
			b.WriteString(in + "\n")
		}
		b.WriteString(in)
		b.Write(encode(m.Key, "", ""))
		b.WriteString(": ")
		if sub, ok := m.Value.(Object); ok {
			writeObject(b, sub, in)
			continue
		}
		b.Write(encode(m.Value, "", ""))
	}
	b.WriteString("\n" + indent + "}")
}
