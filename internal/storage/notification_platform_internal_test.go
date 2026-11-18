package storage

import "testing"

// platformOnly is the queue's skip (#1987): a rule whose when is the platform
// flag alone cannot hold on a fleet component, so it costs no facts there.
// Anything more is compiled and run, as any rule is.
func TestPlatformOnlyIsTheBarePlatformFlag(t *testing.T) {
	t.Parallel()
	for when, want := range map[string]bool{
		"platform":                        true,
		"  platform\n":                    true,
		"":                                false,
		"!platform":                       false,
		"platform && severity_rank >= 40": false,
		"platform || severity_rank >= 40": false,
		"severity_rank >= 40":             false,
		"platform_name == 'x'":            false,
		"(platform)":                      false,
	} {
		if got := platformOnly(when); got != want {
			t.Errorf("platformOnly(%q) = %v, want %v", when, got, want)
		}
	}
}
