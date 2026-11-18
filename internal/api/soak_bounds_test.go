package api

import (
	"testing"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// The soak verdict reads the platform system's alarm history and must know how
// far back and how many that read serves: a silent change to either would make
// a verdict judge a part of the soak, or miss that the read was cut short.
func TestSoakAlarmReadBoundsMatchTheAPI(t *testing.T) {
	t.Parallel()
	if lab.SoakAlarmWindow != systemAlarmWindow {
		t.Errorf("lab.SoakAlarmWindow = %s, the API serves %s", lab.SoakAlarmWindow, systemAlarmWindow)
	}
	if lab.SoakAlarmCap != systemAlarmLimit {
		t.Errorf("lab.SoakAlarmCap = %d, the API serves %d", lab.SoakAlarmCap, systemAlarmLimit)
	}
}
