//go:build linux

package impair

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

// The simulator owns one nftables table and one root qdisc on loopback, and
// nothing else: Apply replaces them whole and Clear removes them, so the
// network outside them is never touched.
const (
	tableName = "omniglass_sim"
	qdiscMajr = 0x5f1 // the simulator's tc handle, 5f1: on lo
)

// Netlink applies plans through the kernel's netlink interfaces (nftables
// for dropping, tc for shaping), with no nft or tc binary: the release image
// is distroless.
type Netlink struct{}

// NewNetlink returns the kernel applier. It needs NET_ADMIN in the network
// namespace the simulator shares with its node.
func NewNetlink() *Netlink { return &Netlink{} }

// Apply makes the network realize the plan, replacing whatever the simulator
// applied before.
func (n *Netlink) Apply(p Plan) error {
	if err := applyFirewall(p); err != nil {
		return fmt.Errorf("impair: firewall: %w", err)
	}
	if err := applyShaping(p.Shaped); err != nil {
		return fmt.Errorf("impair: shaping: %w", err)
	}
	return nil
}

// Clear removes everything the simulator applied.
func (n *Netlink) Clear() error {
	return errors.Join(dropFirewall(), dropShaping())
}

func dropFirewall() error {
	c, err := nftables.New()
	if err != nil {
		return err
	}
	tables, err := c.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == tableName {
			c.DelTable(t)
		}
	}
	return c.Flush()
}

// applyFirewall builds the simulator's table: traffic to an unplugged device
// is dropped (a dark device: connects time out, datagrams vanish, pings go
// unanswered), and an ICMP echo to a loopback address that is no device is
// dropped, so ping answers only for devices.
func applyFirewall(p Plan) error {
	if err := dropFirewall(); err != nil {
		return err
	}
	c, err := nftables.New()
	if err != nil {
		return err
	}
	t := c.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: tableName})
	devices := &nftables.Set{Table: t, Name: "devices", KeyType: nftables.TypeIPAddr}
	unplugged := &nftables.Set{Table: t, Name: "unplugged", KeyType: nftables.TypeIPAddr}
	if err := c.AddSet(devices, elements(p.Devices)); err != nil {
		return err
	}
	if err := c.AddSet(unplugged, elements(p.Unplugged)); err != nil {
		return err
	}
	accept := nftables.ChainPolicyAccept
	ch := c.AddChain(&nftables.Chain{Name: "input", Table: t, Type: nftables.ChainTypeFilter,
		Hooknum: nftables.ChainHookInput, Priority: nftables.ChainPriorityFilter, Policy: &accept})

	ipv4 := []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.NFPROTO_IPV4}},
	}
	daddr := &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4}
	drop := &expr.Verdict{Kind: expr.VerdictDrop}

	// ip daddr @unplugged drop
	c.AddRule(&nftables.Rule{Table: t, Chain: ch, Exprs: append(append([]expr.Any{}, ipv4...),
		daddr,
		&expr.Lookup{SourceRegister: 1, SetName: unplugged.Name, SetID: unplugged.ID},
		drop,
	)})
	// icmp echo-request to 127.0.0.0/8, not 127.0.0.1, not @devices: drop
	c.AddRule(&nftables.Rule{Table: t, Chain: ch, Exprs: append(append([]expr.Any{}, ipv4...),
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_ICMP}},
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{8}}, // echo request
		daddr,
		&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: []byte{255, 0, 0, 0}, Xor: []byte{0, 0, 0, 0}},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{127, 0, 0, 0}},
		daddr,
		&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: []byte{127, 0, 0, 1}},
		&expr.Lookup{SourceRegister: 1, SetName: devices.Name, SetID: devices.ID, Invert: true},
		drop,
	)})
	return c.Flush()
}

func elements(addrs []netip.Addr) []nftables.SetElement {
	out := make([]nftables.SetElement, 0, len(addrs))
	for _, a := range addrs {
		if a.Is4() {
			b := a.As4()
			out = append(out, nftables.SetElement{Key: b[:]})
		}
	}
	return out
}

func loopback() (netlink.Link, error) {
	return netlink.LinkByName("lo")
}

func dropShaping() error {
	lo, err := loopback()
	if err != nil {
		return err
	}
	qs, err := netlink.QdiscList(lo)
	if err != nil {
		return err
	}
	for _, q := range qs {
		if q.Attrs().Parent == netlink.HANDLE_ROOT && q.Attrs().Handle == netlink.MakeHandle(qdiscMajr, 0) {
			return netlink.QdiscDel(q)
		}
	}
	return nil
}

// applyShaping hangs one class per degraded device off a root htb on
// loopback: the class carries the device's rate limit, a netem under it the
// delay, jitter and loss, and a u32 filter sends traffic bound for the
// device's address into it. Only the direction toward the device is shaped,
// so a latency of N adds N to a round trip.
func applyShaping(shaped []Shape) error {
	if err := dropShaping(); err != nil {
		return err
	}
	if len(shaped) == 0 {
		return nil
	}
	lo, err := loopback()
	if err != nil {
		return err
	}
	idx := lo.Attrs().Index
	root := netlink.MakeHandle(qdiscMajr, 0)
	if err := netlink.QdiscAdd(netlink.NewHtb(netlink.QdiscAttrs{LinkIndex: idx, Handle: root, Parent: netlink.HANDLE_ROOT})); err != nil {
		return fmt.Errorf("root qdisc: %w", err)
	}
	for i, s := range shaped {
		minor := uint16(i + 1)
		class := netlink.MakeHandle(qdiscMajr, minor)
		rate := uint64(10_000_000_000) // bits per second; unshaped: 10 Gbit/s
		if s.RateKbit > 0 {
			rate = uint64(s.RateKbit) * 1000
		}
		if err := netlink.ClassAdd(netlink.NewHtbClass(netlink.ClassAttrs{LinkIndex: idx, Parent: root, Handle: class},
			netlink.HtbClassAttrs{Rate: rate, Ceil: rate})); err != nil {
			return fmt.Errorf("class for %s: %w", s.Addr, err)
		}
		if err := netlink.QdiscAdd(netlink.NewNetem(netlink.QdiscAttrs{LinkIndex: idx, Parent: class, Handle: netlink.MakeHandle(0x600+uint16(i), 0)},
			netlink.NetemQdiscAttrs{
				Latency: uint32(s.LatencyMs) * 1000,
				Jitter:  uint32(s.JitterMs) * 1000,
				Loss:    float32(s.LossPct),
			})); err != nil {
			return fmt.Errorf("netem for %s: %w", s.Addr, err)
		}
		b := s.Addr.As4()
		if err := netlink.FilterAdd(&netlink.U32{
			FilterAttrs: netlink.FilterAttrs{LinkIndex: idx, Parent: root, Priority: 1, Protocol: unix.ETH_P_IP},
			ClassId:     class,
			Sel: &netlink.TcU32Sel{Flags: nl.TC_U32_TERMINAL, Nkeys: 1,
				Keys: []netlink.TcU32Key{{Mask: 0xffffffff, Val: binary.BigEndian.Uint32(b[:]), Off: 16}}},
		}); err != nil {
			return fmt.Errorf("filter for %s: %w", s.Addr, err)
		}
	}
	return nil
}
