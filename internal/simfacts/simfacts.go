// Package simfacts renders the simulator's catalog as docs facts (#2837): the
// families, their models, the faces each serves, the behaviors each runs, and
// the accounting of the seeded products no family serves. Everything here is
// read from the typed family specs and the seed's own render, so a family added,
// a model row added or a product accounted for changes the simulation page
// through `make gen`, and a stale committed file fails the drift test.
package simfacts

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/sim/device"
	"github.com/hyperscaleav/omniglass/sim/spec"
)

// Facts is what the docs render of the simulator's catalog.
type Facts struct {
	Families []Family `json:"families"`
	Coverage Coverage `json:"coverage"`
}

// Family is one Go device family: one wire API, its models the rows of its
// table.
type Family struct {
	// Package is the family's directory under sim/device.
	Package string `json:"package"`
	// Name is the family's id, the profile name a fleet snapshot carries.
	Name         string     `json:"name"`
	Manufacturer string     `json:"manufacturer,omitempty"`
	DeviceType   string     `json:"deviceType"`
	Summary      string     `json:"summary"`
	Interfaces   []Face     `json:"interfaces"`
	Models       []Model    `json:"models"`
	Behaviors    []Behavior `json:"behaviors,omitempty"`
}

// Face is one interface a family serves: its protocol and port, whether it is
// behind TLS, and how it authenticates.
type Face struct {
	Name       string `json:"name"`
	Protocol   string `json:"protocol"`
	Port       int    `json:"port,omitempty"`
	TLS        bool   `json:"tls,omitempty"`
	Auth       string `json:"auth,omitempty"`
	Persistent bool   `json:"persistent,omitempty"`
}

// Model is one row of a family's table, with what the seed says of the product
// it simulates: the driver that collects it and its component type. A product
// the seed does not ship carries neither (the alignment test refuses one).
type Model struct {
	Product       string `json:"product"`
	Model         string `json:"model,omitempty"`
	Summary       string `json:"summary,omitempty"`
	Driver        string `json:"driver,omitempty"`
	ComponentType string `json:"componentType,omitempty"`
}

// Behavior is one tick a family runs: its name, how often, and the state paths
// it writes.
type Behavior struct {
	Name     string   `json:"name"`
	Interval string   `json:"interval"`
	Writes   []string `json:"writes,omitempty"`
}

// Coverage accounts for the seeded products on the network that no family
// serves yet (each with its issue) and for the ones with nothing on the network
// to simulate (each with how the fleet watches it instead).
type Coverage struct {
	Unsimulated []Account `json:"unsimulated"`
	Passive     []Account `json:"passive"`
}

// Account is one product and the sentence that accounts for it.
type Account struct {
	Product string `json:"product"`
	Why     string `json:"why"`
}

// Of reads the shipped catalog: every family and the coverage accounting,
// joined to the seed's products for driver and component type.
func Of() (Facts, error) {
	raw, err := seed.FactsJSON()
	if err != nil {
		return Facts{}, err
	}
	var seeded struct {
		Products []struct {
			ID            string `json:"id"`
			DriverID      string `json:"driver_id"`
			ComponentType string `json:"component_type"`
		} `json:"products"`
	}
	if err := json.Unmarshal(raw, &seeded); err != nil {
		return Facts{}, fmt.Errorf("simfacts: read the seed's products: %w", err)
	}
	type product struct{ driver, componentType string }
	byID := map[string]product{}
	for _, p := range seeded.Products {
		byID[p.ID] = product{p.DriverID, p.ComponentType}
	}

	fams, dirs := device.Families(), device.Dirs()
	if len(fams) != len(dirs) {
		return Facts{}, fmt.Errorf("simfacts: %d families but %d directories: run make gen", len(fams), len(dirs))
	}
	var out Facts
	for i, s := range fams {
		out.Families = append(out.Families, familyOf(s, dirs[i], func(id string) (string, string) {
			p := byID[id]
			return p.driver, p.componentType
		}))
	}
	sort.Slice(out.Families, func(i, j int) bool { return out.Families[i].Package < out.Families[j].Package })

	cov := device.Accounted()
	out.Coverage.Unsimulated, out.Coverage.Passive = accounts(cov.Unsimulated), accounts(cov.Passive)
	return out, nil
}

func familyOf(s spec.Spec, dir string, seeded func(product string) (driver, componentType string)) Family {
	f := Family{
		Package: dir, Name: s.Name, Manufacturer: s.Identity.Manufacturer,
		DeviceType: s.Identity.DeviceType, Summary: s.Identity.Summary,
		Interfaces: []Face{}, Models: []Model{},
	}
	for _, i := range s.Interfaces {
		face := Face{Name: i.Name, Protocol: i.Protocol, Port: i.Port, TLS: i.TLS, Persistent: i.Persistent}
		if i.Auth.Enforce || i.Auth.Type != "" {
			face.Auth = i.Auth.Type
		}
		f.Interfaces = append(f.Interfaces, face)
	}
	for _, m := range s.Models {
		driver, ct := seeded(m.Product)
		f.Models = append(f.Models, Model{Product: m.Product, Model: m.Model, Summary: m.Summary, Driver: driver, ComponentType: ct})
	}
	for _, t := range s.Ticks {
		f.Behaviors = append(f.Behaviors, Behavior{Name: t.Name, Interval: duration(t.Interval), Writes: append([]string(nil), t.Writes...)})
	}
	return f
}

// duration writes a tick's interval as the console does: whole seconds as 30s.
func duration(d time.Duration) string {
	if d%time.Second == 0 {
		return fmt.Sprintf("%ds", int64(d/time.Second))
	}
	return d.String()
}

func accounts(m map[string]string) []Account {
	out := []Account{}
	for product, why := range m {
		out = append(out, Account{Product: product, Why: strings.TrimSpace(why)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Product < out[j].Product })
	return out
}

// JSON is the facts as the generated file holds them: indented, sorted and
// stable run to run.
func JSON() ([]byte, error) {
	f, err := Of()
	if err != nil {
		return nil, err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
