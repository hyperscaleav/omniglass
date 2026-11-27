package fleet

import "fmt"

// Address is the i-th address of the simulated block, 127.60.0.1 onward: the
// block every simulated device is placed in, handed out in order. It lives
// here, a leaf, so what plans the fleet (sim/simfleet) and what plans a
// world of it (the lab's scenario) read one definition.
func Address(i int) string {
	return fmt.Sprintf("127.60.%d.%d", i/250, i%250+1)
}
