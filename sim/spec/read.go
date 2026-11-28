package spec

import (
	"fmt"
	"strconv"
)

// The typed reads of a state path a family's handlers, surfaces and ticks make.
// State is a tree of untyped values (a number may be an int, an int64 or a
// float64 depending on who wrote it); these read one as the protocol servers
// always have, so a value reads the same however it was written.

// String reads a path as text: a string as itself, a boolean as true or false,
// a missing path as empty, anything else as its default formatting.
func String(d Device, path string) string { return text(d.Read(path)) }

func text(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprintf("%v", x)
	}
}

// Int64 reads a path as a whole number: a float is truncated toward zero, a
// boolean is 1 or 0, anything not a number is 0.
func Int64(d Device, path string) int64 { return whole(d.Read(path)) }

// Int reads a path as Int64 does.
func Int(d Device, path string) int { return int(Int64(d, path)) }

// Whole reads a value out of a state tree as a whole number, as Int64 reads a
// path: a float is truncated toward zero, a boolean is 1 or 0, anything not a
// number is 0.
func Whole(v any) int64 { return whole(v) }

func whole(v any) int64 {
	switch x := v.(type) {
	case int:
		return int64(x)
	case int32:
		return int64(x)
	case int64:
		return x
	case uint:
		return int64(x)
	case uint32:
		return int64(x)
	case uint64:
		return int64(x)
	case float32:
		return int64(x)
	case float64:
		return int64(x)
	case bool:
		if x {
			return 1
		}
	}
	return 0
}

// Float reads a path as a number; anything not a number is 0.
func Float(d Device, path string) float64 {
	switch x := d.Read(path).(type) {
	case float64:
		return x
	case float32:
		return float64(x)
	}
	return float64(Int64(d, path))
}

// Bool reads a path as a flag: a boolean as itself, the text "true" as true,
// anything else false.
func Bool(d Device, path string) bool {
	switch x := d.Read(path).(type) {
	case bool:
		return x
	case string:
		return x == "true"
	}
	return false
}
