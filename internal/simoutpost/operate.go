package simnode

import (
	"encoding/json"
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/sim"
)

// answer runs an operation the server forwarded from the console against
// the device it names.
func answer(devices []*sim.Device, op collection.SimOperation) collection.SimOperationResult {
	for _, d := range devices {
		if d.DeviceID != op.ComponentID {
			continue
		}
		r := d.Operate(sim.Operation{Kind: op.Kind, Method: op.Method, Path: op.Path, Body: op.Body,
			Command: op.Command, Protocol: op.Protocol, OID: op.OID})
		return collection.SimOperationResult(r)
	}
	return collection.SimOperationResult{Error: "the simulator is not serving this device; it may not have pulled it yet"}
}

// safeAnswer runs an operation so that a panic inside a device becomes an
// error reply: nats runs this callback with no recover, and a panic would end
// the simulator and every device it serves.
func safeAnswer(run func() collection.SimOperationResult) (res collection.SimOperationResult) {
	defer func() {
		if r := recover(); r != nil {
			res = collection.SimOperationResult{Error: fmt.Sprintf("the device failed answering: %v", r)}
		}
	}()
	return run()
}

// subscribeOperations answers the server's operation requests for this
// node's devices.
func subscribeOperations(nc *nats.Conn, name string, engine *sim.Engine) (*nats.Subscription, error) {
	return nc.Subscribe(collection.SimopSubject(name), func(m *nats.Msg) {
		var op collection.SimOperation
		res := collection.SimOperationResult{Error: "a malformed operation"}
		if json.Unmarshal(m.Data, &op) == nil {
			res = safeAnswer(func() collection.SimOperationResult { return answer(engine.Devices(), op) })
		}
		b, err := json.Marshal(res)
		if err != nil {
			return
		}
		_ = m.Respond(b)
	})
}
