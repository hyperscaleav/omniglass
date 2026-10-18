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
	if err := queueDropTable(c); err != nil {
		return err
	}
	return c.Flush()
}

// queueDropTable queues the deletion of the simulator's table, if it exists,
// on c's pending batch.
func queueDropTable(c *nftables.Conn) error {
	tables, err := c.ListTablesOfFamily(nftables.TableFamilyINet)
	if err != nil {
		return err
	}
	for _, t := range tables {
		if t.Name == tableName {
			c.DelTable(t)
		}
	}
	return nil
}

// applyFirewall builds the simulator's table: traffic to an unplugged device
// is dropped (a dark device: connects time out, datagrams vanish, pings go
// unanswered), and an ICMP echo to a loopback address that is no device is
// dropped, so ping answers only for devices. The old table goes and the new
// one arrives in one batch, so an unplugged device never answers in between.
func applyFirewall(p Plan) error {
	c, err := nftables.New()
	if err != nil {
		return err
	}
	if err := queueDropTable(c); err != nil {
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

// maxShaped bounds how many devices can be degraded at once: each takes two
// htb classes and a netem handle, numbered from fixed bases.
const maxShaped = 16_000

// directQlen is the htb root's queue for unclassified traffic (everything on
// loopback that is not a degraded device's). Loopback normally queues
// nothing, so the root is given room enough never to drop it.
var directQlen uint32 = 1 << 16

// applyShaping hangs classes off a root htb on loopback for each degraded
// device. Traffic toward the device takes a class carrying its rate limit
// with a netem under it for the delay, jitter and loss, so a latency of N
// adds N to a round trip. Traffic from the device takes a second class with
// only the rate limit, so the limit holds both ways (a collector's download
// as well as its request) without counting the latency twice.
func applyShaping(shaped []Shape) error {
	if err := dropShaping(); err != nil {
		return err
	}
	if len(shaped) == 0 {
		return nil
	}
	if len(shaped) > maxShaped {
		return fmt.Errorf("%d degraded devices, more than the %d one simulator can shape", len(shaped), maxShaped)
	}
	lo, err := loopback()
	if err != nil {
		return err
	}
	idx := lo.Attrs().Index
	root := netlink.MakeHandle(qdiscMajr, 0)
	htb := netlink.NewHtb(netlink.QdiscAttrs{LinkIndex: idx, Handle: root, Parent: netlink.HANDLE_ROOT})
	htb.DirectQlen = &directQlen
	if err := netlink.QdiscAdd(htb); err != nil {
		return fmt.Errorf("root qdisc: %w", err)
	}
	for i, s := range shaped {
		rate := uint64(10_000_000_000) // bits per second; unlimited: 10 Gbit/s
		if s.RateKbit > 0 {
			rate = uint64(s.RateKbit) * 1000
		}
		b := s.Addr.As4()
		addr := binary.BigEndian.Uint32(b[:])
		toward := netlink.MakeHandle(qdiscMajr, uint16(2*i+1))
		if err := addClass(idx, root, toward, rate); err != nil {
			return fmt.Errorf("class toward %s: %w", s.Addr, err)
		}
		if err := netlink.QdiscAdd(netlink.NewNetem(netlink.QdiscAttrs{LinkIndex: idx, Parent: toward, Handle: netlink.MakeHandle(0x600+uint16(i), 0)},
			netlink.NetemQdiscAttrs{
				Latency: uint32(s.LatencyMs) * 1000,
				Jitter:  uint32(s.JitterMs) * 1000,
				Loss:    float32(s.LossPct),
			})); err != nil {
			return fmt.Errorf("netem for %s: %w", s.Addr, err)
		}
		if err := addFilter(idx, root, toward, addr, 16); err != nil { // destination address
			return fmt.Errorf("filter toward %s: %w", s.Addr, err)
		}
		if s.RateKbit == 0 {
			continue
		}
		from := netlink.MakeHandle(qdiscMajr, uint16(2*i+2))
		if err := addClass(idx, root, from, rate); err != nil {
			return fmt.Errorf("class from %s: %w", s.Addr, err)
		}
		if err := addFilter(idx, root, from, addr, 12); err != nil { // source address
			return fmt.Errorf("filter from %s: %w", s.Addr, err)
		}
	}
	return nil
}

func addClass(idx int, root, handle uint32, rate uint64) error {
	return netlink.ClassAdd(netlink.NewHtbClass(netlink.ClassAttrs{LinkIndex: idx, Parent: root, Handle: handle},
		netlink.HtbClassAttrs{Rate: rate, Ceil: rate}))
}

// addFilter steers IPv4 packets whose address at off (16 the destination,
// 12 the source) is addr into class.
func addFilter(idx int, root, class, addr uint32, off int32) error {
	return netlink.FilterAdd(&netlink.U32{
		FilterAttrs: netlink.FilterAttrs{LinkIndex: idx, Parent: root, Priority: 1, Protocol: unix.ETH_P_IP},
		ClassId:     class,
		Sel: &netlink.TcU32Sel{Flags: nl.TC_U32_TERMINAL, Nkeys: 1,
			Keys: []netlink.TcU32Key{{Mask: 0xffffffff, Val: addr, Off: off}}},
	})
}
