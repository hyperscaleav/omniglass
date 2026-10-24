package driver

import (
	"fmt"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/transport"
)

// The tls transport's verification mode (#1035, decision 3 of #1076) is a
// driver input, so an operator edits it in place like a host or a port and
// it is part of the connection fingerprint through the endpoint's inputs.
// The driver declares its default (the seeded newtron-nvp-tls pins on first
// use, since the gear it speaks to self-signs); a driver that declares none
// verifies; skip is never a default, only an operator's explicit choice.
const TLSModeInput = "tls-mode"

// The tls verification modes.
const (
	// TLSModeVerify checks the certificate against the node's system roots,
	// with the server name taken from the host input.
	TLSModeVerify = "verify"
	// TLSModePin trusts the first public key the component offers and
	// refuses any other after it, as the ssh host key is pinned.
	TLSModePin = "pin"
	// TLSModeSkip checks nothing: the operator's explicit choice, shown on
	// the endpoint.
	TLSModeSkip = "skip"
)

// TLSModes is every mode, in the order the docs and refusals name them.
func TLSModes() []string { return []string{TLSModeVerify, TLSModePin, TLSModeSkip} }

// ParseTLSMode reads a mode as the node does: empty is verify (a driver that
// declares no mode, or a bare endpoint that sets none), anything else must
// be one of the modes.
func ParseTLSMode(v string) (string, error) {
	if v == "" {
		return TLSModeVerify, nil
	}
	if !slices.Contains(TLSModes(), v) {
		return "", fmt.Errorf("tls mode %q is not one of %s", v, strings.Join(TLSModes(), ", "))
	}
	return v, nil
}

// validateTLSInputs refuses a tls-mode input the engine could not read: one
// on a transport with no tls, one that is not a string, or a default that is
// not a mode or is skip.
func (s *Spec) validateTLSInputs() error {
	for _, in := range s.Inputs {
		if in.Name != TLSModeInput {
			continue
		}
		if !transport.UsesTLS(s.Transport) {
			return fmt.Errorf("driver: input %s sets the tls verification mode, and %q has no tls", TLSModeInput, s.Transport)
		}
		if in.Kind != "string" {
			return fmt.Errorf("driver: input %s is a string (one of %s)", TLSModeInput, strings.Join(TLSModes(), ", "))
		}
		if in.Default == "" {
			continue
		}
		if in.Default == TLSModeSkip {
			return fmt.Errorf("driver: input %s cannot default to skip: skipping verification is an operator's explicit choice, never a default", TLSModeInput)
		}
		if _, err := ParseTLSMode(in.Default); err != nil {
			return fmt.Errorf("driver: input %s default: %v", TLSModeInput, err)
		}
	}
	return nil
}

// CheckInputValue refuses a value an attach or an in-place edit supplies for
// an input the engine reads with meaning: the tls mode must be a mode, a udp
// listen port a port. Any other input's value is the driver's to read.
func (s *Spec) CheckInputValue(name, value string) error {
	if name == TLSModeInput && transport.UsesTLS(s.Transport) {
		if _, err := ParseTLSMode(value); err != nil {
			return fmt.Errorf("input %s: %v", TLSModeInput, err)
		}
	}
	if name == ListenPortInput && s.Transport == "udp" {
		if err := checkPort(value); err != nil {
			return fmt.Errorf("input %s: %v", ListenPortInput, err)
		}
	}
	return nil
}
