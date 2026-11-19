// Package settingsfacts installs the settings-reflection seam config
// needs to expand the OMNIGLASS_SETTING__ family in the generated env-var
// reference (#2223). Importing this package wires config.SettingsEnvFacts
// to settings.EnvSettingFacts; configgen and the registry's drift test
// import it so their render carries the per-key rows. Decoupling the two
// this way keeps internal/config free of a settings import, so tools and
// tests compile the registry without the Settings struct.
package settingsfacts

import (
	"github.com/hyperscaleav/omniglass/internal/config"
	"github.com/hyperscaleav/omniglass/internal/settings"
)

func init() {
	config.SettingsEnvFacts = func() []config.SettingsEnvFact {
		facts := settings.EnvSettingFacts()
		out := make([]config.SettingsEnvFact, len(facts))
		for i, f := range facts {
			out[i] = config.SettingsEnvFact{Key: f.Key, Default: f.Default, Doc: f.Doc}
		}
		return out
	}
}
