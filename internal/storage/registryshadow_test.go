package storage

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

// TestShadowOverlayDistinguishesAbsentFromNull pins the one rule the shadow
// overlay turns on: a key the image does not carry falls back to the official
// row, and a key the image carries as null sets the column to null. On
// component_type those are different answers, because null on stem/icon/abbrev
// already means "inherit from the parent" (component_types.go), so collapsing
// the two would make a forked child silently re-adopt an official value the
// operator cleared, or make a column added after the fork resolve to nothing.
func TestShadowOverlayDistinguishesAbsentFromNull(t *testing.T) {
	official := ComponentType{
		Name: "mic", DisplayName: "Microphone",
		Stem: strPtr("mic"), Icon: strPtr("mic"), Abbrev: strPtr("mic"),
		DefaultTags: []string{"audio"}, Official: true,
	}

	cases := []struct {
		name  string
		image string
		want  func(ComponentType) ComponentType
	}{
		{
			name:  "no image leaves the official row alone",
			image: "",
			want:  func(ct ComponentType) ComponentType { return ct },
		},
		{
			name:  "an absent key falls back to the official value",
			image: `{"display_name":"Mikrofon"}`,
			want: func(ct ComponentType) ComponentType {
				ct.DisplayName = "Mikrofon"
				return ct
			},
		},
		{
			name:  "a null key clears the column, it does not fall back",
			image: `{"stem":null}`,
			want: func(ct ComponentType) ComponentType {
				ct.Stem = nil
				return ct
			},
		},
		{
			name:  "a valued key overrides",
			image: `{"stem":"microphone","icon":"mic-2","abbrev":"mc"}`,
			want: func(ct ComponentType) ComponentType {
				ct.Stem, ct.Icon, ct.Abbrev = strPtr("microphone"), strPtr("mic-2"), strPtr("mc")
				return ct
			},
		},
		{
			name:  "an empty tag set is a real value, not an absent key",
			image: `{"default_tags":[]}`,
			want: func(ct ComponentType) ComponentType {
				ct.DefaultTags = []string{}
				return ct
			},
		},
		{
			name:  "the structural columns are never overlaid",
			image: `{"id":"00000000-0000-0000-0000-000000000000","name":"other","official":false,"parent_id":"00000000-0000-0000-0000-000000000000"}`,
			want:  func(ct ComponentType) ComponentType { return ct },
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh official row per case, and the overlay must not touch it.
			// encoding/json writes THROUGH a non-nil pointer, so an overlay that
			// decoded into the official row's own *string would rewrite the
			// string every reader of that row shares. Reusing one row across
			// the cases would hide exactly that: each case would compare
			// against an expectation already corrupted the same way.
			in := official
			in.Stem, in.Icon, in.Abbrev = strPtr(*official.Stem), strPtr(*official.Icon), strPtr(*official.Abbrev)
			in.DefaultTags = append([]string(nil), official.DefaultTags...)

			got, err := applyComponentTypeImage(in, []byte(tc.image))
			if err != nil {
				t.Fatalf("applyComponentTypeImage: %v", err)
			}
			want := tc.want(official)
			if !sameComponentType(got, want) {
				t.Fatalf("overlay = %s, want %s", showComponentType(got), showComponentType(want))
			}
			if *in.Stem != *official.Stem || *in.Icon != *official.Icon || *in.Abbrev != *official.Abbrev {
				t.Fatalf("the overlay wrote through the official row's pointers: stem/icon/abbrev now %q/%q/%q, want %q/%q/%q",
					*in.Stem, *in.Icon, *in.Abbrev, *official.Stem, *official.Icon, *official.Abbrev)
			}
			if len(in.DefaultTags) != len(official.DefaultTags) || (len(in.DefaultTags) > 0 && in.DefaultTags[0] != official.DefaultTags[0]) {
				t.Fatalf("the overlay wrote through the official row's tag slice: %v, want %v", in.DefaultTags, official.DefaultTags)
			}
		})
	}
}

// TestShadowOverlayRefusesAGarbledImage keeps a corrupt image from silently
// resolving to the official row: the read fails loudly instead.
func TestShadowOverlayRefusesAGarbledImage(t *testing.T) {
	if _, err := applyComponentTypeImage(ComponentType{Name: "mic"}, []byte(`{"stem":`)); err == nil {
		t.Fatal("applyComponentTypeImage accepted a truncated image, want an error")
	}
}

// TestComponentTypeShadowImageIsWholeRow proves decision 2: a fork captures
// every mutable column, not only the edited one, so a later release's change
// to an unedited column does not reach a forked row.
func TestComponentTypeShadowImageIsWholeRow(t *testing.T) {
	image, err := componentTypeShadowImage(ComponentType{
		DisplayName: "Microphone", Stem: strPtr("mic"), Icon: nil, Abbrev: strPtr("mic"),
		DefaultTags: []string{"audio"},
	})
	if err != nil {
		t.Fatalf("componentTypeShadowImage: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(image, &keys); err != nil {
		t.Fatalf("decode image: %v", err)
	}
	for _, k := range []string{"display_name", "stem", "icon", "abbrev", "default_tags"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("image is missing mutable column %q; a fork captures the whole row", k)
		}
	}
	// A nil column is captured as an explicit null, not dropped: null is a
	// value on this registry ("inherit from the parent"), so dropping it would
	// re-inherit the official row's on the next read.
	if string(keys["icon"]) != "null" {
		t.Errorf("image icon = %s, want an explicit null", keys["icon"])
	}
	for _, k := range []string{"id", "name", "official", "parent_id"} {
		if _, ok := keys[k]; ok {
			t.Errorf("image carries structural column %q; a shadow never moves a row or renames it", k)
		}
	}
}

func strPtr(s string) *string { return &s }

func sameComponentType(a, b ComponentType) bool {
	return a.ID == b.ID && a.Name == b.Name && a.DisplayName == b.DisplayName &&
		samePtr(a.Stem, b.Stem) && samePtr(a.Icon, b.Icon) && samePtr(a.Abbrev, b.Abbrev) &&
		sameTags(a.DefaultTags, b.DefaultTags) && a.Official == b.Official && sameUUIDPtr(a.ParentID, b.ParentID)
}

func sameUUIDPtr(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func samePtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func sameTags(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return (a == nil) == (b == nil)
}

func showComponentType(ct ComponentType) string {
	b, _ := json.Marshal(map[string]any{
		"display_name": ct.DisplayName, "stem": ct.Stem, "icon": ct.Icon,
		"abbrev": ct.Abbrev, "default_tags": ct.DefaultTags, "name": ct.Name,
	})
	return string(b)
}
