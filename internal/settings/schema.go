// Package settings: schema.go is the single source of truth for the settings
// document. Each namespace is a struct of tagged fields; adding a setting is one
// field. Reflection over Settings (settings.go) builds the code-defaults layer and
// the namespace registry from these tags, so there is no second place to keep in
// sync.
package settings

import "reflect"

// Settings is the canonical settings document, one field per namespace. The
// settings tag is "<domain>[,client]": domain is "profile" or "platform", and the
// client token marks a client-visible namespace (fed to /settings/me).
type Settings struct {
	UI          UISettings    `json:"ui" settings:"profile,client"`
	Keybindings Keybindings   `json:"keybindings" settings:"profile,client"`
	Label       LabelSettings `json:"label" settings:"platform,client"`
}

// UISettings is the ui namespace.
type UISettings struct {
	Theme          string `json:"theme" enum:"omniglass-dark,omniglass-light" default:"omniglass-dark" doc:"Console color theme"`
	DefaultLanding string `json:"default_landing" pattern:"^/" default:"/" doc:"Route the console opens to (an absolute path)"`
}

// Keybindings is the keymap namespace: a closed set of developer-defined actions.
type Keybindings struct {
	OpenDetail     string `json:"open_detail" default:"d" doc:"Open the detail blade"`
	OpenEdit       string `json:"open_edit" default:"e" doc:"Open the edit pane"`
	CloseBlade     string `json:"close_blade" default:"Escape" doc:"Close the top blade"`
	CommandPalette string `json:"command_palette" default:"mod+k" doc:"Open the command palette"`
}

// LabelSettings is the label namespace: how the label rule engine renders, as
// opposed to WHAT it renders (the rules themselves are per-tier columns on the
// registries, not a setting).
//
// It is the first PLATFORM-domain namespace, and the first that is platform and
// client-visible at once. The two halves say different things and both are
// wanted here: platform domain means one install-wide list an admin owns, never
// cascading to a group or a user, because a label is stored once and read by
// everybody, so a per-user dictionary would be a per-user spelling of a shared
// row. Client-visible means any authenticated caller may READ the effective
// list, which is what lets the console render a label the same way the server
// did without asking for the admin settings read.
type LabelSettings struct {
	// Acronyms is the dictionary the rule engine's `title` function consults.
	// Matching is whole-word and case-insensitive, and the entry is the form
	// emitted, so "PoE" titles "poe" and "Poe" alike as "PoE".
	//
	// The tag is huma's comma-separated array form, which coerceDefault parses
	// into the same list huma reflects into the schema.
	//
	// nullable:"false" turns off huma's default array nullability. A list-valued
	// setting is empty or populated, never null: null is the merge patch's delete
	// sentinel, and a schema that admitted it would be inviting a write that means
	// something else entirely.
	Acronyms []string `json:"acronyms" nullable:"false" default:"AV,BYOD,DSP,EDID,HDBaseT,HDCP,HDMI,IP,IR,KVM,LAN,LCD,LED,MTR,NDI,NVR,OLED,PC,PoE,PTZ,RF,SDI,SIP,TV,UC,USB,VC,VLAN,4K,8K" doc:"Words a generated label title-cases to a fixed form, whole-word and case-insensitive"`
}

// namespaceType indexes each namespace's json name to its sub-struct type, for the
// write validator (validate.go). Built once by reflecting Settings.
var namespaceType = func() map[string]reflect.Type {
	m := map[string]reflect.Type{}
	t := reflect.TypeOf(Settings{})
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		m[jsonName(f)] = f.Type
	}
	return m
}()

// jsonName returns a struct field's json name (the tag before any comma, else the
// Go field name).
func jsonName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if i := indexByte(tag, ','); i >= 0 {
		tag = tag[:i]
	}
	return tag
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
