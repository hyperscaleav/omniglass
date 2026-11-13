package devseed

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/hyperscaleav/omniglass/internal/driver"
	"github.com/hyperscaleav/omniglass/internal/storage"
)

// The induced refusal (#1698): a scale lab can make one simulated device
// answer a property key no catalog registers, so the server refuses its
// samples as refused-unregistered and the delivery table reads a loss with a
// known cause. A driver's emitted names are checked against the catalogs when
// the driver is written, so the key is registered first, baked into the
// device's task, and then deleted: the task keeps the name, the node keeps
// collecting it, and the server has no type to land it on.
const (
	// InducedKey is the property key the induced device answers.
	InducedKey = "lab-induced-unregistered"
	// InducedPoll is the poll the induced driver adds to the official one.
	InducedPoll = "induced-unregistered"
	// InducedEvery is the induced poll's cadence: short, so a measurement
	// window of a minute sees the refusals whatever the official cadences.
	InducedEvery = "10s"
)

// Induced names what an induced seed attached: the component (its plan key,
// system/role/n) whose endpoint answers Key through Driver.
type Induced struct {
	Component, Driver, Key string
}

// InduceSpec returns sp with one more poll, InducedPoll, that asks the first
// plain property poll's question again every InducedEvery and answers it under
// key, reading the value the way that poll's first property emit does; from
// names that emit. A poll over records or one that carries an identity is not
// plain (its answer is a list, or it binds a device): it is skipped. ok is
// false when no poll qualifies. The official polls are left as they are, so
// the device collects everything it did and the one key besides.
func InduceSpec(sp driver.Spec, key string, isProperty func(string) bool) (out driver.Spec, from string, ok bool) {
	for _, p := range sp.Polls {
		if p.Records != "" || p.Identity != nil {
			continue
		}
		for _, em := range p.Emits {
			if em.Log != nil || em.Name == "" || !isProperty(em.Name) {
				continue
			}
			copied := driver.Emit{Name: key, Extract: em.Extract, Transform: em.Transform}
			out = sp
			out.Polls = append(slices.Clone(sp.Polls), driver.Poll{
				Name: InducedPoll, Schedule: driver.Schedule{Every: InducedEvery},
				Request: p.Request, Emits: []driver.Emit{copied},
			})
			return out, em.Name, true
		}
	}
	return sp, "", false
}

// inducer attaches the one induced endpoint of a seed: the first planned
// endpoint whose official driver InduceSpec can extend.
type inducer struct {
	gw      storage.Gateway
	actorID string
	done    *Induced
	// tried holds the official drivers already looked at, so each is read
	// once however many endpoints attach through it.
	tried map[string]bool
}

// driverFor returns the driver the planned endpoint attaches through: the
// induced one, made the first time a planned driver can be induced, or the
// planned one.
func (in *inducer) driverFor(ctx context.Context, official string) (string, error) {
	if in.done != nil || in.tried[official] {
		return official, nil
	}
	in.tried[official] = true
	d, err := in.gw.GetDriver(ctx, official)
	if err != nil {
		return "", fmt.Errorf("devseed: read driver %s to induce: %w", official, err)
	}
	if d.Spec == nil {
		return official, nil
	}
	sp, err := driver.Parse(d.Spec)
	if err != nil {
		return "", fmt.Errorf("devseed: parse driver %s to induce: %w", official, err)
	}
	props, err := in.gw.ListPropertyTypes(ctx)
	if err != nil {
		return "", fmt.Errorf("devseed: list properties to induce: %w", err)
	}
	isProperty := func(n string) bool {
		return slices.ContainsFunc(props, func(p storage.PropertyType) bool { return p.Name == n })
	}
	induced, from, ok := InduceSpec(*sp, InducedKey, isProperty)
	if !ok {
		return official, nil
	}
	src, err := in.gw.GetPropertyType(ctx, from)
	if err != nil {
		return "", fmt.Errorf("devseed: read property %s to induce: %w", from, err)
	}
	if _, err := in.gw.CreatePropertyType(ctx, in.actorID, storage.PropertyTypeSpec{
		Name: InducedKey, Label: "Lab induced unregistered", DataType: src.DataType,
		Description: "Registered only long enough to bake into the lab's induced device; deleted before the lab collects.",
	}); err != nil {
		return "", fmt.Errorf("devseed: register the induced key: %w", err)
	}
	raw, err := json.Marshal(induced)
	if err != nil {
		return "", fmt.Errorf("devseed: encode the induced driver: %w", err)
	}
	name := "lab-induced-" + official
	if _, err := in.gw.CreateDriver(ctx, in.actorID, storage.Driver{
		Name: name, Label: d.Label + " (induced refusal)", Version: d.Version, Spec: raw,
	}); err != nil {
		return "", fmt.Errorf("devseed: create the induced driver: %w", err)
	}
	in.done = &Induced{Driver: name, Key: InducedKey}
	return name, nil
}

// finish takes the induced key out of the catalog once its endpoint is
// attached, which is what makes its samples a refusal.
func (in *inducer) finish(ctx context.Context) (*Induced, error) {
	if in.done == nil {
		return nil, nil
	}
	if err := in.gw.DeletePropertyType(ctx, in.actorID, InducedKey); err != nil {
		return nil, fmt.Errorf("devseed: unregister the induced key: %w", err)
	}
	return in.done, nil
}
