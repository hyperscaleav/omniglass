// Package nvp is the NVP room surface in Go (#2831): what a Newtron panel
// answers on its line protocol (TCP, CRLF lines), for every device that
// answers it. GET INPUT and GET SCENE report the room, SET INPUT and SET
// SCENE change it (the line must match whole, so each allowed value is a
// command of its own), and SUBSCRIBE INPUT, LOG and SCENE push what changes.
// It reproduces profiles/surfaces/nvp-room.yaml byte for byte, quirks
// included: only INPUT can be unsubscribed.
package nvp

import "github.com/hyperscaleav/omniglass/sim/surface"

// The state paths the surface reads, writes and pushes; the family keeps its
// state there.
const (
	PathInput = "status.video.input"
	PathLog   = "status.diagnostics.log"
	PathScene = "status.room.scene"
)

// Field is one typed state value the surface reads and writes.
type Field[T any] struct {
	Get func() T
	Set func(T)
}

// Room is the surface: the inputs and scenes it allows, and the state it
// reads and writes.
type Room struct {
	Inputs []string
	Scenes []string
	Input  Field[string]
	Scene  Field[string]
}

// DefaultInputs and DefaultScenes are what the Newtron devices allow.
var (
	DefaultInputs = []string{"hdmi1", "hdmi2", "hdmi3", "dp1", "usbc"}
	DefaultScenes = []string{"idle", "presentation", "vc"}
)

// Commands lists the lines the surface answers, in help order.
func (r Room) Commands() []string {
	out := []string{"GET INPUT"}
	for _, in := range r.Inputs {
		out = append(out, "SET INPUT "+in)
	}
	out = append(out, "SUBSCRIBE INPUT", "UNSUBSCRIBE INPUT", "SUBSCRIBE LOG", "GET SCENE")
	for _, s := range r.Scenes {
		out = append(out, "SET SCENE "+s)
	}
	return append(out, "SUBSCRIBE SCENE")
}

// Handle answers one line.
func (r Room) Handle(line string) (surface.Reply, bool) {
	switch line {
	case "GET INPUT":
		return surface.Reply{Text: "INPUT " + r.Input.Get()}, true
	case "GET SCENE":
		return surface.Reply{Text: "SCENE " + r.Scene.Get()}, true
	case "SUBSCRIBE INPUT":
		return subscribe(PathInput, "INPUT"), true
	case "UNSUBSCRIBE INPUT":
		return surface.Reply{Text: "OK", Action: surface.ActionUnsubscribe, Subject: PathInput}, true
	case "SUBSCRIBE LOG":
		return subscribe(PathLog, "LOG"), true
	case "SUBSCRIBE SCENE":
		return subscribe(PathScene, "SCENE"), true
	}
	if in, ok := suffix(line, "SET INPUT ", r.Inputs); ok {
		r.Input.Set(in)
		return surface.Reply{Text: "INPUT " + in, Changes: []surface.Change{{Path: PathInput, Value: in}}}, true
	}
	if sc, ok := suffix(line, "SET SCENE ", r.Scenes); ok {
		r.Scene.Set(sc)
		return surface.Reply{Text: "SCENE " + sc, Changes: []surface.Change{{Path: PathScene, Value: sc}}}, true
	}
	return surface.Reply{}, false
}

func subscribe(path, token string) surface.Reply {
	return surface.Reply{Text: "OK", Action: surface.ActionSubscribe, Subject: path, Token: token}
}

// suffix reports the allowed value a line carries after prefix.
func suffix(line, prefix string, allowed []string) (string, bool) {
	if len(line) <= len(prefix) || line[:len(prefix)] != prefix {
		return "", false
	}
	v := line[len(prefix):]
	for _, a := range allowed {
		if a == v {
			return v, true
		}
	}
	return "", false
}

// InputSubject is the video input's subject, the one a panel's SSH, TLS and
// Telnet faces each declare beside the raw port's full set.
func InputSubject() surface.Subject {
	return surface.Subject{Match: PathInput, Path: PathInput, Token: "INPUT",
		Push: func(v any) string { return "EVT INPUT " + surface.Raw(v) }}
}

// Subjects is the whole set: the input, the diagnostics log and the scene.
func Subjects() []surface.Subject {
	return []surface.Subject{
		InputSubject(),
		{Match: PathLog, Path: PathLog, Token: "LOG",
			Push: func(v any) string { return "LOG " + surface.Raw(v) }},
		{Match: PathScene, Path: PathScene, Token: "SCENE",
			Push: func(v any) string { return "EVT SCENE " + surface.Raw(v) }},
	}
}
