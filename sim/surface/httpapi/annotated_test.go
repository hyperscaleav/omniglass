package httpapi

import "testing"

// An annotated document is Document's layout with the comment lines a template
// left in it, nested blocks included (#2834).
func TestAnnotatedKeepsTheCommentLinesATemplateLeft(t *testing.T) {
	got := string(Annotated(Object{
		{Key: "device", Value: Object{
			{Key: "power", Value: "on"},
			{Key: "cpu", Value: 14, Noted: true},
			{Key: "temp", Value: 44.5, Noted: true},
		}},
		{Key: "recent", Value: []string{"a&b", "c"}, Noted: true},
	}))
	want := "{\n  \"device\": {\n    \"power\": \"on\",\n    \n    \"cpu\": 14,\n    \n    \"temp\": 44.5\n  },\n  \n  \"recent\": [\"a&b\",\"c\"]\n}\n"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	// With nothing noted it is the plain document.
	type doc struct {
		A int    `json:"a"`
		B string `json:"b"`
	}
	if got, want := string(Annotated(Object{{Key: "a", Value: 1}, {Key: "b", Value: "x"}})), string(Document(doc{1, "x"})); got != want {
		t.Fatalf("plain: got %q want %q", got, want)
	}
}
