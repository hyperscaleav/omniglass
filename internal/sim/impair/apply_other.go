//go:build !linux

package impair

import "errors"

// Netlink is unavailable off Linux: impairments need the kernel's nftables
// and tc.
type Netlink struct{}

// NewNetlink returns an applier that refuses: only Linux can impair.
func NewNetlink() *Netlink { return &Netlink{} }

var errUnsupported = errors.New("impair: impairments need Linux (nftables and tc)")

// Apply refuses off Linux.
func (n *Netlink) Apply(Plan) error { return errUnsupported }

// Clear has nothing to remove off Linux.
func (n *Netlink) Clear() error { return nil }
