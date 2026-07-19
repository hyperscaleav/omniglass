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
	UI          UISettings  `json:"ui" settings:"profile,client"`
	Keybindings Keybindings `json:"keybindings" settings:"profile,client"`
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
